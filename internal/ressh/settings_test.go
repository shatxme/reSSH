package ressh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsDefaults(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir(), SettingsFile: filepath.Join(t.TempDir(), "settings.json")}
	settings, err := LoadSettings(paths)
	if err != nil {
		t.Fatal(err)
	}
	if settings.SocksPort != 1080 {
		t.Fatalf("expected default socks port 1080, got %d", settings.SocksPort)
	}
	if !settings.AutoProxy {
		t.Fatal("expected auto proxy enabled by default")
	}
}

func TestSaveAndLoadSettings(t *testing.T) {
	configDir := t.TempDir()
	paths := Paths{ConfigDir: configDir, SettingsFile: filepath.Join(configDir, "settings.json")}
	input := Settings{DefaultTarget: "ressh-2", SocksPort: 2080, AutoProxy: false}
	if err := SaveSettings(paths, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSettings(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != input {
		t.Fatalf("loaded settings mismatch: %+v != %+v", loaded, input)
	}
	if info, err := os.Stat(paths.SettingsFile); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected settings mode 0600, got %o", info.Mode().Perm())
	}
}
