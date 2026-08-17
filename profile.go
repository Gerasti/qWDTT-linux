package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProfileData struct {
	PeerAddr     string   `json:"peer"`
	Password     string   `json:"password"`
	Hashes       []string `json:"hashes"`
	Listen       string   `json:"listen,omitempty"`
	TurnHost     string   `json:"turn,omitempty"`
	TurnPort     string   `json:"turnPort,omitempty"`
	Workers      int      `json:"workers,omitempty"`
	DeviceID     string   `json:"device_id,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	LinkFile     string   `json:"link_file,omitempty"`     // Path to file containing wdtt:// or qwdtt:// URL
	Groups       []string `json:"groups,omitempty"`        // Labels for organizing profiles
	Subscription string   `json:"subscription,omitempty"`  // Name of subscription managing this profile
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

	// Clean password: trim whitespace and remove literal \n sequences
	p.Password = strings.TrimSpace(p.Password)
	p.Password = strings.ReplaceAll(p.Password, "\\n", "")
	p.Password = strings.ReplaceAll(p.Password, "\n", "")
	p.Password = strings.ReplaceAll(p.Password, "\r", "")

	// If peer/password are empty but LinkFile is set, read and parse the link file
	if (p.PeerAddr == "" || p.Password == "") && p.LinkFile != "" {
		linkData, err := os.ReadFile(p.LinkFile)
		if err != nil {
			return nil, fmt.Errorf("профиль %q: не удалось прочитать link_file %q: %w", name, p.LinkFile, err)
		}

		link, err := parseLink(strings.TrimSpace(string(linkData)))
		if err != nil {
			return nil, fmt.Errorf("профиль %q: не удалось распарсить link_file %q: %w", name, p.LinkFile, err)
		}

		// Populate fields from parsed link
		p.PeerAddr = link.IP + ":" + link.DTLSPort
		p.Password = link.Password
		p.Hashes = link.Hashes
		if link.Workers > 0 {
			p.Workers = link.Workers
		}
		if link.Port != "" {
			p.Listen = "127.0.0.1:" + link.Port
		}
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

	// Subscription-managed profiles must always carry their subscription group
	if p.Subscription != "" {
		found := false
		for _, g := range p.Groups {
			if g == p.Subscription {
				found = true
				break
			}
		}
		if !found {
			p.Groups = append(p.Groups, p.Subscription)
		}
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

// renameProfile moves a profile from oldName to newName on disk and
// updates all dependent state: status.json (enabled/disabled),
// active_profile, and autoswitch_current_profile.
func renameProfile(oldName, newName string) error {
	if strings.HasPrefix(oldName, "ro-") || strings.HasPrefix(newName, "ro-") {
		return fmt.Errorf("cannot rename read-only profiles")
	}

	prof, err := loadProfile(oldName)
	if err != nil {
		return err
	}

	if _, err := os.Stat(profilePath(newName)); err == nil {
		return fmt.Errorf("profile %q already exists", newName)
	}

	if err := saveProfile(newName, *prof); err != nil {
		return fmt.Errorf("failed to save profile %q: %w", newName, err)
	}

	if err := os.Remove(profilePath(oldName)); err != nil {
		_ = os.Remove(profilePath(newName))
		return fmt.Errorf("failed to remove old profile %q: %w", oldName, err)
	}

	statuses, err := loadStatuses()
	if err == nil {
		if enabled, exists := statuses[oldName]; exists {
			statuses[newName] = enabled
			delete(statuses, oldName)
			_ = saveStatuses(statuses)
		}
	}

	if getActiveProfile() == oldName {
		_ = setActiveProfile(newName)
	}

	if getAutoswitchCurrentProfile() == oldName {
		_ = setAutoswitchCurrentProfile(newName)
	}

	return nil
}
