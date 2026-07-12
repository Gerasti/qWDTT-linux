package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ProfileData struct {
	PeerAddr string   `json:"peer"`
	Password string   `json:"password"`
	Hashes   []string `json:"hashes"`
	Listen   string   `json:"listen,omitempty"`
	TurnHost string   `json:"turn,omitempty"`
	TurnPort string   `json:"port,omitempty"`
	DeviceID string   `json:"device_id,omitempty"`
	Enabled  bool     `json:"enabled"`
	Priority int      `json:"priority,omitempty"`
}

func profilePath(name string) string {
	return filepath.Join(configDir(), "profiles", name+".json")
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

	var rawMap map[string]interface{}
	if err := json.Unmarshal(data, &rawMap); err == nil {
		if _, exists := rawMap["enabled"]; !exists {
			p.Enabled = true
		}
	}

	return &p, nil
}

func saveProfile(name string, p ProfileData) error {
	dir := filepath.Join(configDir(), "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if p.DeviceID == "" {
		p.DeviceID = getOrCreateDeviceID()
	}

	data, err := json.Marshal(p)
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
