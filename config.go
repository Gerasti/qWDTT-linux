package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func autoswitchProfilePath() string {
	return filepath.Join(pidFilesDir(), "autoswitch_current_profile")
}

func setAutoswitchCurrentProfile(name string) error {
	return os.WriteFile(autoswitchProfilePath(), []byte(name), 0o644)
}

func getAutoswitchCurrentProfile() string {
	data, err := os.ReadFile(autoswitchProfilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func clearAutoswitchCurrentProfile() {
	_ = os.Remove(autoswitchProfilePath())
}

func splitCfgPath(profile string) string {
	return filepath.Join(pidFilesDir(), "qwdtt-"+profile+".bl")
}

func writeSplitCfg(profile string, bl, blFile string, splitRoutes []string) error {
	data, err := json.Marshal(struct {
		BlackList     string   `json:"black_list"`
		BlackListFile string   `json:"black_list_file"`
		SplitRoutes   []string `json:"split_routes"`
	}{bl, blFile, splitRoutes})
	if err != nil {
		return err
	}
	return os.WriteFile(splitCfgPath(profile), data, 0o644)
}

func readSplitCfg(profile string) (bl, blFile string) {
	bl, blFile, _ = readSplitCfgFull(profile)
	return bl, blFile
}

func readSplitCfgFull(profile string) (bl, blFile string, splitRoutes []string) {
	data, err := os.ReadFile(splitCfgPath(profile))
	if err != nil {
		return "", "", nil
	}
	var cfg struct {
		BlackList     string   `json:"black_list"`
		BlackListFile string   `json:"black_list_file"`
		SplitRoutes   []string `json:"split_routes"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", nil
	}
	return cfg.BlackList, cfg.BlackListFile, cfg.SplitRoutes
}

func removeSplitCfg(profile string) {
	_ = os.Remove(splitCfgPath(profile))
}
