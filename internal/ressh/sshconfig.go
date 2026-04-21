package ressh

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SSHHost struct {
	Alias        string
	Hostname     string
	User         string
	Port         int
	IdentityFile string
}

func ListSSHHosts() ([]SSHHost, error) {
	configPath, err := sshConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var hosts []SSHHost
	current := SSHHost{Port: 22}
	flush := func() {
		if current.Alias == "" || strings.ContainsAny(current.Alias, "*?") {
			return
		}
		hosts = append(hosts, current)
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		normalized := strings.ReplaceAll(trimmed, "=", " ")
		parts := strings.Fields(normalized)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(parts[0])
		value := strings.Join(parts[1:], " ")
		switch key {
		case "host":
			flush()
			current = SSHHost{Alias: strings.Fields(value)[0], Port: 22}
		case "hostname":
			current.Hostname = value
		case "user":
			current.User = value
		case "port":
			if port, err := strconv.Atoi(value); err == nil {
				current.Port = port
			}
		case "identityfile":
			current.IdentityFile = value
		}
	}
	flush()

	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })
	return hosts, nil
}

type ResshHostBlock struct {
	Hostname     string
	User         string
	Port         int
	IdentityFile string
	Country      string
	Name         string
}

var resshHostRe = regexp.MustCompile(`^Host\s+(ressh-(?:[a-z]+(?:-[a-z]+)*-)?\d+)\s*$`)

func AppendResshHostBlock(opts ResshHostBlock) (string, error) {
	configPath, err := sshConfigPath()
	if err != nil {
		return "", err
	}
	sshDir := filepath.Dir(configPath)
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return "", err
	}
	content, _ := os.ReadFile(configPath)
	alias := findExistingAlias(string(content), opts.Hostname)
	if alias == "" {
		alias = nextAlias(string(content), opts)
	}
	block := buildBlock(alias, opts)
	if strings.Contains(string(content), "Host "+alias) {
		updated := replaceHostBlock(string(content), alias, block)
		if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
			return "", err
		}
		return alias, nil
	}
	file, err := os.OpenFile(configPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(block); err != nil {
		return "", err
	}
	return alias, nil
}

func sshConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

func findExistingAlias(content, hostname string) string {
	var current string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if match := resshHostRe.FindStringSubmatch(trimmed); match != nil {
			current = match[1]
			continue
		}
		if current == "" {
			continue
		}
		normalized := strings.ReplaceAll(trimmed, "=", " ")
		parts := strings.Fields(normalized)
		if len(parts) >= 2 && strings.EqualFold(parts[0], "HostName") && strings.Join(parts[1:], " ") == hostname {
			return current
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
			current = ""
		}
	}
	return ""
}

func nextAlias(content string, opts ResshHostBlock) string {
	max := 0
	for _, line := range strings.Split(content, "\n") {
		match := resshHostRe.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		numberPart := regexp.MustCompile(`(\d+)$`).FindString(match[1])
		if numberPart == "" {
			continue
		}
		n, _ := strconv.Atoi(numberPart)
		if n > max {
			max = n
		}
	}
	prefix := "ressh-"
	if opts.Name != "" {
		if slug := slugify(opts.Name); slug != "" {
			prefix = "ressh-" + slug + "-"
		}
	} else if opts.Country != "" {
		if slug := slugify(opts.Country); slug != "" {
			prefix = "ressh-" + slug + "-"
		}
	}
	return fmt.Sprintf("%s%d", prefix, max+1)
}

func buildBlock(alias string, opts ResshHostBlock) string {
	return fmt.Sprintf("\n# Added by reSSH - %s\nHost %s\n  HostName %s\n  User %s\n  Port %d\n  IdentityFile %s\n",
		time.Now().Format("2006-01-02"), alias, opts.Hostname, opts.User, opts.Port, opts.IdentityFile)
}

func replaceHostBlock(content, alias, block string) string {
	lines := strings.Split(content, "\n")
	var out []string
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "Host "+alias) {
			if len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "# Added by reSSH") {
				out = out[:len(out)-1]
			}
			skipping = true
			continue
		}
		if skipping {
			if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
				skipping = false
			} else {
				continue
			}
		}
		out = append(out, line)
	}
	result := strings.TrimRight(strings.Join(out, "\n"), "\n") + block
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
