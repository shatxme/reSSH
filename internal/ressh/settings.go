package ressh

import (
	"encoding/json"
	"os"
)

type Settings struct {
	DefaultTarget string `json:"default_target"`
	SocksPort     int    `json:"socks_port"`
	AutoProxy     bool   `json:"auto_proxy"`
}

func LoadSettings(paths Paths) (Settings, error) {
	settings := Settings{SocksPort: 1080, AutoProxy: true}
	data, err := os.ReadFile(paths.SettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return Settings{}, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	if settings.SocksPort == 0 {
		settings.SocksPort = 1080
	}
	return settings, nil
}

func SaveSettings(paths Paths, settings Settings) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(paths.SettingsFile, append(data, '\n'), 0o600)
}
