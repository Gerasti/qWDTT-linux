package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProfileData struct {
	PeerAddr string   `json:"peer"`
	Password string   `json:"password"`
	Hashes   []string `json:"hashes"`
	Listen   string   `json:"listen,omitempty"`
	TurnHost string   `json:"turn,omitempty"`
	TurnPort string   `json:"port,omitempty"`
	DeviceID string   `json:"device_id,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

func profilePath(name string) string {
	if strings.HasPrefix(name, "ro-") {
		return filepath.Join(configDir(), "ro-profiles", name+".json")
	}
	return filepath.Join(configDir(), "profiles", name+".json")
}

func statusFilePath() string {
	return filepath.Join(configDir(), "status.json")
}

func loadStatuses() (map[string]bool, error) {
	data, err := os.ReadFile(statusFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]bool), nil
		}
		return nil, err
	}
	var statuses map[string]bool
	if err := json.Unmarshal(data, &statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func saveStatuses(statuses map[string]bool) error {
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statusFilePath(), data, 0o600)
}

func isProfileEnabled(name string) bool {
	statuses, err := loadStatuses()
	if err != nil {
		return true // default enabled
	}
	if enabled, exists := statuses[name]; exists {
		return enabled
	}
	return true // default enabled
}

func setProfileEnabled(name string, enabled bool) error {
	statuses, err := loadStatuses()
	if err != nil {
		statuses = make(map[string]bool)
	}
	statuses[name] = enabled
	return saveStatuses(statuses)
}

func loadProfile(name string) (*ProfileData, error) {
	data, err := os.ReadFile(profilePath(name))
	if err != nil {
		return nil, fmt.Errorf("профиль %q: %w", name, err)
	}
	var p ProfileData
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("профиль %q parse: %w", name, err)
	}
	return &p, nil
}

func saveProfile(name string, p ProfileData) error {
	// Prevent saving ro-profiles
	if strings.HasPrefix(name, "ro-") {
		return fmt.Errorf("cannot modify read-only profile")
	}

	dir := filepath.Join(configDir(), "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if p.DeviceID == "" {
		p.DeviceID = getOrCreateDeviceID()
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(profilePath(name), data, 0o600)
}

func maskPassword(pwd string) string {
	if len(pwd) <= 6 {
		return "****"
	}
	return pwd[:3] + "****" + pwd[len(pwd)-3:]
}
