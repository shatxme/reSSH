package ressh

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

var retryDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

type TargetSpec struct {
	Alias    string `json:"alias,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
}

type Status struct {
	Status           string `json:"status"`
	ConnectedTo      string `json:"connected_to"`
	SocksPort        int    `json:"socks_port"`
	ProxyEnabled     bool   `json:"proxy_enabled"`
	KillSwitchActive bool   `json:"kill_switch_active"`
	LastError        string `json:"last_error,omitempty"`
}

type connectRequest struct {
	Target       TargetSpec `json:"target"`
	ProxyEnabled bool       `json:"proxy_enabled"`
}

type TunnelManager struct {
	mu           sync.Mutex
	settings     Settings
	status       Status
	sshCmd       *exec.Cmd
	proxyService string
	lastTarget   TargetSpec
	lastProxy    bool
	intentional  bool
	retryCount   int
	retryTimer   *time.Timer
	paths        Paths
}

func NewTunnelManager(paths Paths) (*TunnelManager, error) {
	settings, err := LoadSettings(paths)
	if err != nil {
		return nil, err
	}
	return &TunnelManager{
		settings: settings,
		paths:    paths,
		status: Status{
			Status:    "disconnected",
			SocksPort: settings.SocksPort,
		},
	}, nil
}

func (m *TunnelManager) Connect(spec TargetSpec, proxyEnabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Status != "disconnected" {
		return errors.New("tunnel already active")
	}
	if spec.Port == 0 {
		spec.Port = 22
	}
	m.lastTarget = spec
	m.lastProxy = proxyEnabled
	m.intentional = false
	m.retryCount = 0
	return m.startLocked(spec, proxyEnabled)
}

func (m *TunnelManager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intentional = true
	m.killRetryLocked()
	if m.sshCmd != nil && m.sshCmd.Process != nil {
		_ = m.sshCmd.Process.Kill()
	}
	if m.proxyService != "" {
		if err := disableProxy(m.proxyService); err != nil {
			log.Printf("proxy disable failed: %v", err)
		}
		m.proxyService = ""
	}
	m.status.Status = "disconnected"
	m.status.KillSwitchActive = false
	m.status.ProxyEnabled = false
	m.status.LastError = ""
	log.Printf("disconnected")
	return nil
}

func (m *TunnelManager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *TunnelManager) startLocked(spec TargetSpec, proxyEnabled bool) error {
	args := []string{
		"-D", fmt.Sprint(m.settings.SocksPort),
		"-N",
		"-C",
		"-o", "TCPKeepAlive=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
	}
	label := spec.Alias
	if spec.Alias != "" {
		args = append(args, spec.Alias)
	} else {
		if spec.KeyFile != "" {
			args = append(args, "-i", ExpandPath(spec.KeyFile))
		}
		label = spec.User + "@" + spec.Hostname
		args = append(args, "-p", fmt.Sprint(spec.Port), label)
	}

	cmd := exec.Command("ssh", args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		m.status.LastError = err.Error()
		return err
	}

	m.sshCmd = cmd
	m.status.Status = "connecting"
	m.status.ConnectedTo = label
	m.status.LastError = ""
	log.Printf("connecting to %s", label)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("ssh: %s", scanner.Text())
		}
	}()
	go m.watchProcess(cmd)
	go m.waitForTunnel(cmd, proxyEnabled)
	return nil
}

func (m *TunnelManager) watchProcess(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sshCmd == cmd {
		m.sshCmd = nil
	}
	if m.intentional {
		return
	}
	if err != nil {
		m.status.LastError = err.Error()
		log.Printf("ssh exited: %v", err)
	}
	wasConnected := m.status.Status == "connected"
	m.status.Status = "disconnected"
	m.status.ProxyEnabled = m.proxyService != ""
	if wasConnected {
		m.status.KillSwitchActive = true
		log.Printf("connection lost; keeping proxy enabled until reconnect")
	}
	m.scheduleRetryLocked()
}

func (m *TunnelManager) waitForTunnel(cmd *exec.Cmd, proxyEnabled bool) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if !checkLocalPort(m.settings.SocksPort) {
			continue
		}
		m.mu.Lock()
		if m.sshCmd != cmd || m.status.Status != "connecting" {
			m.mu.Unlock()
			return
		}
		m.status.Status = "connected"
		m.status.KillSwitchActive = false
		m.retryCount = 0
		if proxyEnabled && m.proxyService == "" {
			service, err := enableProxy(m.settings.SocksPort)
			if err != nil {
				m.status.LastError = err.Error()
				log.Printf("proxy enable failed: %v", err)
			} else {
				m.proxyService = service
				m.status.ProxyEnabled = true
			}
		} else if !proxyEnabled {
			m.status.ProxyEnabled = false
		}
		log.Printf("connected to %s", m.status.ConnectedTo)
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sshCmd == cmd && m.status.Status == "connecting" {
		m.status.LastError = "timed out waiting for tunnel"
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

func (m *TunnelManager) scheduleRetryLocked() {
	m.killRetryLocked()
	delay := retryDelays[min(m.retryCount, len(retryDelays)-1)]
	m.retryCount++
	log.Printf("reconnecting in %s", delay)
	m.retryTimer = time.AfterFunc(delay, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.intentional {
			return
		}
		if err := m.startLocked(m.lastTarget, m.lastProxy); err != nil {
			m.status.LastError = err.Error()
			log.Printf("reconnect failed: %v", err)
			m.scheduleRetryLocked()
		}
	})
}

func (m *TunnelManager) killRetryLocked() {
	if m.retryTimer != nil {
		m.retryTimer.Stop()
		m.retryTimer = nil
	}
}

func checkLocalPort(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ExpandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := userHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

func userHomeDir() (string, error) {
	return os.UserHomeDir()
}
