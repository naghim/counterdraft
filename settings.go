package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// settings is the small set of preferences remembered between runs.
type settings struct {
	// Vision controls screen reading: when the GSI payload has no draft data
	// (all-pick modes), the overlay can read the enemy picks off the top bar.
	Vision bool `json:"vision"`
}

// defaultSettings is what a first run uses: screen reading on.
func defaultSettings() settings { return settings{Vision: true} }

func settingsPath() string {
	return filepath.Join(xdg.ConfigHome, "counterdraft", "settings.json")
}

// loadSettings reads the preferences file, falling back to defaults when it is
// missing or unreadable. A corrupt file must not stop the program.
func loadSettings() settings {
	s := defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultSettings()
	}
	return s
}

// saveSettings writes the preferences file, creating its directory if needed.
func saveSettings(s settings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
