package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings holds user preferences that persist across sessions, stored next to
// the recents under the user config dir.
type settings struct {
	Voice bool `json:"voice"` // read the thread aloud (macOS); off by default
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "oh-my-reddit", "settings.json")
}

// loadSettings reads saved preferences, falling back to zero values (voice off)
// when there's no file yet or it can't be read.
func loadSettings() settings {
	var s settings
	p := settingsPath()
	if p == "" {
		return s
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func saveSettings(s settings) {
	p := settingsPath()
	if p == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}
