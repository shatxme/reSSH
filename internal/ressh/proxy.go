package ressh

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"os/exec"
	"runtime"
	"strings"
)

func enableProxy(port int) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return enableMacProxy(port)
	case "linux":
		return enableLinuxProxy(port)
	case "windows":
		return enableWindowsProxy(port)
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func disableProxy(service string) error {
	switch runtime.GOOS {
	case "darwin":
		return disableMacProxy(service)
	case "linux":
		return disableLinuxProxy()
	case "windows":
		return disableWindowsProxy()
	default:
		return nil
	}
}

func enableMacProxy(port int) (string, error) {
	service := detectMacService()
	if err := run("sudo", "-n", "/usr/sbin/networksetup", "-setsocksfirewallproxy", service, "127.0.0.1", fmt.Sprint(port)); err == nil {
		if err := run("sudo", "-n", "/usr/sbin/networksetup", "-setsocksfirewallproxystate", service, "on"); err == nil {
			return service, nil
		}
	}

	currentUser, err := user.Current()
	if err != nil {
		return service, err
	}
	commands := []string{
		fmt.Sprintf("/usr/sbin/networksetup -setsocksfirewallproxy %s 127.0.0.1 %d", shellQuoteMac(service), port),
		fmt.Sprintf("/usr/sbin/networksetup -setsocksfirewallproxystate %s on", shellQuoteMac(service)),
		"mkdir -p /etc/sudoers.d",
		fmt.Sprintf("printf '%%s\\n' %s > /etc/sudoers.d/ressh", shellQuoteMac(currentUser.Username+" ALL=(root) NOPASSWD: /usr/sbin/networksetup")),
		"chmod 0440 /etc/sudoers.d/ressh",
	}
	if err := runOsascript(strings.Join(commands, " && ")); err != nil {
		return service, err
	}
	return service, nil
}

func disableMacProxy(service string) error {
	if err := run("sudo", "-n", "/usr/sbin/networksetup", "-setsocksfirewallproxystate", service, "off"); err == nil {
		return nil
	}
	return runOsascript(fmt.Sprintf("/usr/sbin/networksetup -setsocksfirewallproxystate %s off", shellQuoteMac(service)))
}

func detectMacService() string {
	cmd := exec.Command("/usr/sbin/networksetup", "-listallnetworkservices")
	output, err := cmd.Output()
	if err != nil {
		return "Wi-Fi"
	}
	var services []string
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "An asterisk") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		services = append(services, trimmed)
	}
	for _, service := range services {
		if macServiceHasIP(service) {
			return service
		}
	}
	for _, preferred := range []string{"Wi-Fi", "Ethernet", "USB 10/100/1000 LAN"} {
		for _, service := range services {
			if service == preferred {
				return service
			}
		}
	}
	if len(services) > 0 {
		return services[0]
	}
	return "Wi-Fi"
}

func macServiceHasIP(service string) bool {
	cmd := exec.Command("/usr/sbin/networksetup", "-getinfo", service)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "ip address:") {
			continue
		}
		ip := strings.TrimSpace(strings.TrimPrefix(trimmed, "IP address:"))
		return ip != "" && strings.ToLower(ip) != "none"
	}
	return false
}

func runOsascript(command string) error {
	escaped := strings.ReplaceAll(command, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, escaped)
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("osascript: %s", trimmed)
	}
	return nil
}

func shellQuoteMac(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func enableWindowsProxy(port int) (string, error) {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	if err := run("reg", "add", key, "/v", "ProxyServer", "/t", "REG_SZ", "/d", fmt.Sprintf("socks=127.0.0.1:%d", port), "/f"); err != nil {
		return "", err
	}
	if err := run("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f"); err != nil {
		return "", err
	}
	return "windows", nil
}

func disableWindowsProxy() error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	return run("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f")
}

func enableLinuxProxy(port int) (string, error) {
	desktop := detectLinuxDesktop()
	switch desktop {
	case "gnome":
		if err := run("gsettings", "set", "org.gnome.system.proxy", "mode", "manual"); err != nil {
			return "", err
		}
		if err := run("gsettings", "set", "org.gnome.system.proxy.socks", "host", "127.0.0.1"); err != nil {
			return "", err
		}
		if err := run("gsettings", "set", "org.gnome.system.proxy.socks", "port", fmt.Sprint(port)); err != nil {
			return "", err
		}
	case "kde":
		bin, err := exec.LookPath("kwriteconfig6")
		if err != nil {
			bin, err = exec.LookPath("kwriteconfig5")
		}
		if err != nil {
			return "", errors.New("kwriteconfig not found")
		}
		if err := run(bin, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "ProxyType", "1"); err != nil {
			return "", err
		}
		if err := run(bin, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "socksProxy", fmt.Sprintf("socks://127.0.0.1:%d", port)); err != nil {
			return "", err
		}
	default:
		return "", errors.New("no supported Linux desktop proxy integration found")
	}
	_ = os.Setenv("ALL_PROXY", fmt.Sprintf("socks5://127.0.0.1:%d", port))
	return desktop, nil
}

func disableLinuxProxy() error {
	switch detectLinuxDesktop() {
	case "gnome":
		if err := run("gsettings", "set", "org.gnome.system.proxy", "mode", "none"); err != nil {
			return err
		}
	case "kde":
		bin, err := exec.LookPath("kwriteconfig6")
		if err != nil {
			bin, err = exec.LookPath("kwriteconfig5")
		}
		if err != nil {
			return err
		}
		if err := run(bin, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "ProxyType", "0"); err != nil {
			return err
		}
	}
	_ = os.Unsetenv("ALL_PROXY")
	return nil
}

func detectLinuxDesktop() string {
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	switch {
	case strings.Contains(desktop, "gnome"), strings.Contains(desktop, "unity"), strings.Contains(desktop, "cinnamon"):
		return "gnome"
	case strings.Contains(desktop, "kde"):
		return "kde"
	}
	if _, err := exec.LookPath("gsettings"); err == nil {
		return "gnome"
	}
	if _, err := exec.LookPath("kwriteconfig6"); err == nil {
		return "kde"
	}
	if _, err := exec.LookPath("kwriteconfig5"); err == nil {
		return "kde"
	}
	return "unknown"
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%s: %s", name, trimmed)
	}
	return nil
}
