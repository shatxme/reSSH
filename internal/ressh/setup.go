package ressh

import (
	"bufio"
	"bytes"
	"context"
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

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type VPSSetupInput struct {
	Host     string
	User     string
	Password string
	Name     string
}

type VPSSetupResult struct {
	Alias         string
	KeyFile       string
	DefaultTarget string
}

func ResolvePassword(raw string, passwordStdin bool) (string, error) {
	if raw != "" {
		return raw, nil
	}
	if passwordStdin {
		data, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			return "", err
		}
		secret := strings.TrimSpace(string(data))
		if secret == "" {
			return "", errors.New("stdin password was empty")
		}
		return secret, nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if len(secret) == 0 {
		return "", errors.New("password cannot be empty")
	}
	return string(secret), nil
}

func RunVPSSetup(ctx context.Context, paths Paths, input VPSSetupInput) (VPSSetupResult, error) {
	_ = paths
	if input.Host == "" || input.User == "" || input.Password == "" {
		return VPSSetupResult{}, errors.New("host, user, and password are required")
	}
	keyFile, pubKey, err := ensureKeyPair(input.Host)
	if err != nil {
		return VPSSetupResult{}, err
	}
	client, err := ssh.Dial("tcp", input.Host+":22", &ssh.ClientConfig{
		User:            input.User,
		Auth:            []ssh.AuthMethod{ssh.Password(input.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	})
	if err != nil {
		return VPSSetupResult{}, err
	}
	defer client.Close()

	commands := []string{
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh",
		fmt.Sprintf("grep -qxF %s ~/.ssh/authorized_keys 2>/dev/null || printf '%%s\\n' %s >> ~/.ssh/authorized_keys", shellQuote(pubKey), shellQuote(pubKey)),
		"chmod 600 ~/.ssh/authorized_keys",
		"export PATH=/usr/sbin:/usr/bin:/sbin:/bin:$PATH",
		"command -v apt-get >/dev/null && DEBIAN_FRONTEND=noninteractive apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y ufw || true",
		"command -v ufw >/dev/null && ufw allow 22/tcp && ufw --force enable || true",
	}
	for _, command := range commands {
		if err := remoteExec(ctx, client, command); err != nil {
			return VPSSetupResult{}, err
		}
	}

	country, _ := geolocate(ctx, input.Host)
	alias, err := AppendResshHostBlock(ResshHostBlock{
		Hostname:     input.Host,
		User:         input.User,
		Port:         22,
		IdentityFile: keyFile,
		Country:      country,
		Name:         input.Name,
	})
	if err != nil {
		return VPSSetupResult{}, err
	}
	return VPSSetupResult{Alias: alias, KeyFile: keyFile}, nil
}

func ensureKeyPair(host string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return "", "", err
	}
	base := filepath.Join(sshDir, "ressh_"+strings.ReplaceAll(host, ".", "_"))
	base = filepath.Join(sshDir, "ressh_"+safeFilePart(host))
	pub := base + ".pub"
	if _, err := os.Stat(base); os.IsNotExist(err) {
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", base, "-N", "", "-C", "ressh-"+host)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("ssh-keygen: %s", strings.TrimSpace(string(output)))
		}
	}
	data, err := os.ReadFile(pub)
	if err != nil {
		return "", "", err
	}
	return base, strings.TrimSpace(string(data)), nil
}

func remoteExec(ctx context.Context, client *ssh.Client, command string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	var stderr bytes.Buffer
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	case err := <-done:
		if err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return errors.New(message)
		}
		return nil
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func geolocate(ctx context.Context, host string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ip-api.com/json/"+host+"?fields=country", nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var payload struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Country, nil
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.NewReplacer(":", "_", "/", "_", "\\", "_", "@", "_", " ", "_").Replace(value)
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, value)
	value = strings.Trim(value, "._-")
	if value == "" {
		return "host"
	}
	return value
}
