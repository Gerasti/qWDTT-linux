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

func writeSplitCfg(profile string, bl, blFile string, appliedDomains []string, splitRoutes []string) error {
	data, err := json.Marshal(struct {
		BlackList       string   `json:"black_list"`
		BlackListFile   string   `json:"black_list_file"`
		BlackListMtime  int64    `json:"black_list_mtime"`
		BlackListDomains []string `json:"black_list_domains,omitempty"`
		SplitRoutes     []string `json:"split_routes"`
	}{bl, blFile, blFileMtime(blFile), appliedDomains, splitRoutes})
	if err != nil {
		return err
	}
	return os.WriteFile(splitCfgPath(profile), data, 0o644)
}

// blFileMtime returns the modification time (UnixNano) of the bl-file, or 0 if
// the path is empty / the file cannot be stat'd. It is recorded by
// writeSplitCfg so that callers (connect / BL_RELOAD) can later tell whether the
// file on disk has been edited (e.g. via `qwdtt bl add`/`bl rm` without `-r`)
// since it was last applied to the running daemon.
func blFileMtime(path string) int64 {
	if path == "" {
		return 0
	}
	expanded, err := expandPath(path)
	if err != nil || expanded == "" {
		expanded = path
	}
	if fi, err := os.Stat(expanded); err == nil {
		return fi.ModTime().UnixNano()
	}
	return 0
}

func readSplitCfg(profile string) (bl, blFile string) {
	bl, blFile, _, _, _ = readSplitCfgFull(profile)
	return bl, blFile
}

func readSplitCfgFull(profile string) (bl, blFile string, splitRoutes []string, blMtime int64, appliedDomains []string) {
	data, err := os.ReadFile(splitCfgPath(profile))
	if err != nil {
		return "", "", nil, 0, nil
	}
	var cfg struct {
		BlackList       string   `json:"black_list"`
		BlackListFile   string   `json:"black_list_file"`
		BlackListMtime  int64    `json:"black_list_mtime"`
		BlackListDomains []string `json:"black_list_domains"`
		SplitRoutes     []string `json:"split_routes"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", nil, 0, nil
	}
	return cfg.BlackList, cfg.BlackListFile, cfg.SplitRoutes, cfg.BlackListMtime, cfg.BlackListDomains
}

// removeSplitCfg removes the per-profile state file.
func removeSplitCfg(profile string) {
	_ = os.Remove(splitCfgPath(profile))
}

// stateFileMtime returns the modification time (UnixNano) of the state file
// qwdtt-<profile>.bl, or 0 if it cannot be stat'd. Used as a fallback for
// state files written by older binaries that did not record black_list_mtime.
func stateFileMtime(profile string) int64 {
	if fi, err := os.Stat(splitCfgPath(profile)); err == nil {
		return fi.ModTime().UnixNano()
	}
	return 0
}
