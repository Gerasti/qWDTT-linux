package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	dir := filepath.Join(base, "qwdtt")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func deviceIDPath() string {
	return filepath.Join(configDir(), "device_id")
}

func getOrCreateDeviceID() string {
	path := deviceIDPath()

	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) == 16 {
			return id
		}
	}

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(t >> (i * 8))
		}
	}
	id := hex.EncodeToString(b)

	_ = os.WriteFile(path, []byte(id), 0o600)

	return id
}

func activeProfilePath() string {
	return filepath.Join(configDir(), "active_profile")
}

func setActiveProfile(name string) error {
	return os.WriteFile(activeProfilePath(), []byte(name), 0o644)
}

func getActiveProfile() string {
	data, err := os.ReadFile(activeProfilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func clearActiveProfile() {
	_ = os.Remove(activeProfilePath())
}
