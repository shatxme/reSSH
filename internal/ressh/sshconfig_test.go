package ressh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSSHHostsParsesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `Host work
  HostName example.com
  User alice
  Port 2222
  IdentityFile ~/.ssh/work

Host *
  ForwardAgent no

Host personal
  HostName home.example
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	hosts, err := ListSSHHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Alias != "personal" || hosts[1].Alias != "work" {
		t.Fatalf("unexpected hosts: %+v", hosts)
	}
	if hosts[1].Port != 2222 || hosts[1].IdentityFile != "~/.ssh/work" {
		t.Fatalf("unexpected work host: %+v", hosts[1])
	}
}

func TestAppendResshHostBlockAppendsAndReplaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(sshDir, "config")
	initial := "Host existing\n  HostName keep.me\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	alias, err := AppendResshHostBlock(ResshHostBlock{
		Hostname:     "1.2.3.4",
		User:         "root",
		Port:         22,
		IdentityFile: "~/.ssh/ressh_1_2_3_4",
		Country:      "United States",
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias != "ressh-united-states-1" {
		t.Fatalf("unexpected alias: %s", alias)
	}

	alias, err = AppendResshHostBlock(ResshHostBlock{
		Hostname:     "1.2.3.4",
		User:         "admin",
		Port:         2200,
		IdentityFile: "~/.ssh/updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias != "ressh-united-states-1" {
		t.Fatalf("expected existing alias to be reused, got %s", alias)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Count(content, "Host ressh-united-states-1") != 1 {
		t.Fatalf("expected one rewritten ressh block, got:\n%s", content)
	}
	if !strings.Contains(content, "User admin") || !strings.Contains(content, "Port 2200") || !strings.Contains(content, "IdentityFile ~/.ssh/updated") {
		t.Fatalf("updated block missing expected values:\n%s", content)
	}
	if !strings.Contains(content, "Host existing") {
		t.Fatalf("existing config block was removed:\n%s", content)
	}
}

func TestAppendResshHostBlockFallsBackWhenSlugEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	alias, err := AppendResshHostBlock(ResshHostBlock{
		Hostname:     "8.8.8.8",
		User:         "root",
		Port:         22,
		IdentityFile: "~/.ssh/ressh_8_8_8_8",
		Name:         "---",
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias != "ressh-1" {
		t.Fatalf("unexpected alias: %s", alias)
	}
}
