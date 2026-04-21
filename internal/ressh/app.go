package ressh

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const daemonAddr = "127.0.0.1:47931"

var ErrDaemonUnavailable = errors.New("daemon unavailable")

type App struct {
	paths  Paths
	client *http.Client
}

func New() (*App, error) {
	paths, err := NewPaths()
	if err != nil {
		return nil, err
	}
	return &App{
		paths:  paths,
		client: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func (a *App) RunDaemon(ctx context.Context) error {
	if err := a.paths.Ensure(); err != nil {
		return err
	}
	token, err := ensureToken(a.paths)
	if err != nil {
		return err
	}
	manager, err := NewTunnelManager(a.paths)
	if err != nil {
		return err
	}
	return Serve(ctx, daemonAddr, token, manager)
}

func (a *App) Connect(ctx context.Context, spec TargetSpec, proxyEnabled bool) error {
	if err := a.ensureDaemon(ctx); err != nil {
		return err
	}
	_, err := a.post(ctx, "/connect", connectRequest{Target: spec, ProxyEnabled: proxyEnabled})
	return err
}

func (a *App) Disconnect(ctx context.Context) error {
	if _, err := a.Status(ctx); err != nil {
		if errors.Is(err, ErrDaemonUnavailable) {
			return nil
		}
		return err
	}
	_, err := a.post(ctx, "/disconnect", struct{}{})
	return err
}

func (a *App) Status(ctx context.Context) (Status, error) {
	if err := a.paths.Ensure(); err != nil {
		return Status{}, err
	}
	var status Status
	if err := a.request(ctx, http.MethodGet, "/status", nil, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (a *App) ResolveTarget(raw string) (TargetSpec, string, error) {
	settings, err := LoadSettings(a.paths)
	if err != nil {
		return TargetSpec{}, "", err
	}
	target := strings.TrimSpace(raw)
	if target == "" {
		target = settings.DefaultTarget
	}
	if target == "" {
		return TargetSpec{}, "", errors.New("no target provided and no default target configured")
	}

	hosts, err := ListSSHHosts()
	if err != nil {
		return TargetSpec{}, "", err
	}
	for _, host := range hosts {
		if host.Alias == target {
			return TargetSpec{Alias: host.Alias}, host.Alias, nil
		}
	}

	userPart, hostPart, ok := strings.Cut(target, "@")
	if ok && hostPart != "" {
		return TargetSpec{Hostname: hostPart, User: userPart, Port: 22}, target, nil
	}

	return TargetSpec{}, "", fmt.Errorf("target %q not found in ~/.ssh/config", target)
}

func (a *App) ListTargets() ([]SSHHost, string, error) {
	hosts, err := ListSSHHosts()
	if err != nil {
		return nil, "", err
	}
	settings, err := LoadSettings(a.paths)
	if err != nil {
		return nil, "", err
	}
	return hosts, settings.DefaultTarget, nil
}

func (a *App) SetDefaultTarget(target string) error {
	target = strings.TrimSpace(target)
	hosts, err := ListSSHHosts()
	if err != nil {
		return err
	}
	for _, host := range hosts {
		if host.Alias == target {
			settings, err := LoadSettings(a.paths)
			if err != nil {
				return err
			}
			settings.DefaultTarget = target
			return SaveSettings(a.paths, settings)
		}
	}
	return fmt.Errorf("target %q not found in ~/.ssh/config", target)
}

func (a *App) Logs() (string, error) {
	data, err := os.ReadFile(a.paths.LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (a *App) VPSSetup(ctx context.Context, input VPSSetupInput) (VPSSetupResult, error) {
	result, err := RunVPSSetup(ctx, a.paths, input)
	if err != nil {
		return VPSSetupResult{}, err
	}
	settings, err := LoadSettings(a.paths)
	if err != nil {
		return VPSSetupResult{}, err
	}
	settings.DefaultTarget = result.Alias
	if err := SaveSettings(a.paths, settings); err != nil {
		return VPSSetupResult{}, err
	}
	result.DefaultTarget = result.Alias
	return result, nil
}

func (a *App) ensureDaemon(ctx context.Context) error {
	if _, err := a.Status(ctx); err == nil {
		return nil
	} else if !errors.Is(err, ErrDaemonUnavailable) {
		return err
	}

	if err := a.paths.Ensure(); err != nil {
		return err
	}
	if _, err := ensureToken(a.paths); err != nil {
		return err
	}
	logFile, err := os.OpenFile(a.paths.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if _, err := a.Status(ctx); err == nil {
			return nil
		}
	}
	return errors.New("daemon did not start")
}

func (a *App) post(ctx context.Context, path string, body any) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	var response bytes.Buffer
	if err := a.request(ctx, http.MethodPost, path, &buf, &response); err != nil {
		return nil, err
	}
	return response.Bytes(), nil
}

func (a *App) request(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://"+daemonAddr+path, body)
	if err != nil {
		return err
	}
	if err := a.paths.Ensure(); err != nil {
		return err
	}
	token, err := ensureToken(a.paths)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return ErrDaemonUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusServiceUnavailable {
			return ErrDaemonUnavailable
		}
		message := strings.TrimSpace(string(payload))
		if message == "" {
			message = resp.Status
		}
		return errors.New(message)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	switch target := out.(type) {
	case *bytes.Buffer:
		_, err = io.Copy(target, resp.Body)
		return err
	default:
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

func ensureToken(paths Paths) (string, error) {
	if data, err := os.ReadFile(paths.TokenFile); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(paths.TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

type Paths struct {
	ConfigDir    string
	SettingsFile string
	TokenFile    string
	LogFile      string
}

func NewPaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	dir := filepath.Join(base, "ressh")
	return Paths{
		ConfigDir:    dir,
		SettingsFile: filepath.Join(dir, "settings.json"),
		TokenFile:    filepath.Join(dir, "daemon.token"),
		LogFile:      filepath.Join(dir, "daemon.log"),
	}, nil
}

func (p Paths) Ensure() error {
	return os.MkdirAll(p.ConfigDir, 0o700)
}
