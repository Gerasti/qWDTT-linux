package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SubscriptionConfig struct {
	URL              string   `json:"url"`
	SubscriptionName string   `json:"subscriptionName"`
	Description      string   `json:"description,omitempty"`
	TrafficUsedMb    float64  `json:"trafficUsedMb,omitempty"`
	TrafficLimitMb   float64  `json:"trafficLimitMb,omitempty"`
	UpdatedAt        string   `json:"updatedAt,omitempty"`
	Version          int      `json:"version,omitempty"`
	Profiles         []string `json:"profiles"`
	LastFetch        string   `json:"lastFetch,omitempty"`
}

type SubscriptionJSON struct {
	SubscriptionName string              `json:"subscriptionName"`
	GroupName        string              `json:"groupName"`
	Description      string              `json:"description,omitempty"`
	TrafficUsedMb    float64             `json:"trafficUsedMb,omitempty"`
	TrafficLimitMb   float64             `json:"trafficLimitMb,omitempty"`
	UpdatedAt        string              `json:"updatedAt,omitempty"`
	Version          int                 `json:"version,omitempty"`
	Profiles         []SubscriptionEntry `json:"profiles,omitempty"`
	Servers          []SubscriptionEntry `json:"servers,omitempty"`
}

type SubscriptionEntry struct {
	Name           string `json:"name"`
	Peer           string `json:"peer"`
	Hashes         string `json:"hashes,omitempty"`
	VKHashes       string `json:"vkHashes,omitempty"`
	WorkersPerHash int    `json:"workersPerHash,omitempty"`
	Workers        int    `json:"workers,omitempty"`
	Port           int    `json:"port,omitempty"`
	ListenPort     int    `json:"listenPort,omitempty"`
	Password       string `json:"password,omitempty"`
	Pass           string `json:"pass,omitempty"`
}

func subscriptionDir() string {
	return filepath.Join(configDir(), "subscriptions")
}

func subscriptionPath(name string) string {
	return filepath.Join(subscriptionDir(), name+".json")
}

func loadSubscription(name string) (*SubscriptionConfig, error) {
	data, err := os.ReadFile(subscriptionPath(name))
	if err != nil {
		return nil, fmt.Errorf("подписка %q: %w", name, err)
	}
	var sub SubscriptionConfig
	if err := json.Unmarshal(data, &sub); err != nil {
		return nil, fmt.Errorf("подписка %q parse: %w", name, err)
	}
	return &sub, nil
}

func saveSubscription(name string, sub *SubscriptionConfig) error {
	dir := subscriptionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(subscriptionPath(name), data, 0o600)
}

func listSubscriptions() []string {
	entries, err := os.ReadDir(subscriptionDir())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	return names
}

func isSubscriptionName(name string) bool {
	_, err := os.Stat(subscriptionPath(name))
	return err == nil
}

// profilesInSubscription returns all profile names managed by the given subscription.
func profilesInSubscription(subName string) []string {
	sub, err := loadSubscription(subName)
	if err != nil {
		return nil
	}
	return append([]string(nil), sub.Profiles...)
}

// profilesInAllSubscriptions returns all profile names managed by any subscription.
func profilesInAllSubscriptions() []string {
	var names []string
	for _, subName := range listSubscriptions() {
		names = append(names, profilesInSubscription(subName)...)
	}
	return names
}

func fetchSubscriptionJSON(url string) ([]byte, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("URL должен начинаться с http:// или https://")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить подписку: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ошибка HTTP %d при получении подписки", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ответ: %w", err)
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, fmt.Errorf("пустой ответ от сервера подписки")
	}

	// Base64-декодинг, если ответ не начинается с '{'
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(trimmed)
		if err != nil {
			return nil, fmt.Errorf("ответ не является JSON или Base64: %w", err)
		}
	}
	return decoded, nil
}

func parseSubscriptionJSON(data []byte) (*SubscriptionJSON, error) {
	var sub SubscriptionJSON
	if err := json.Unmarshal(data, &sub); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON подписки: %w", err)
	}

	if sub.SubscriptionName == "" {
		sub.SubscriptionName = sub.GroupName
	}
	if sub.SubscriptionName == "" {
		return nil, fmt.Errorf("subscriptionName (или groupName) обязателен в JSON")
	}

	if len(sub.Profiles) == 0 && len(sub.Servers) > 0 {
		sub.Profiles = sub.Servers
	}

	if len(sub.Profiles) == 0 {
		return nil, fmt.Errorf("в подписке нет профилей")
	}
	return &sub, nil
}

func subscriptionEntryToProfile(entry SubscriptionEntry, subName string) (ProfileData, error) {
	if entry.Peer == "" {
		return ProfileData{}, fmt.Errorf("профиль %q: peer обязателен", entry.Name)
	}

	password := entry.Password
	if password == "" {
		password = entry.Pass
	}
	if password == "" {
		return ProfileData{}, fmt.Errorf("профиль %q: password обязателен", entry.Name)
	}

	hashesStr := entry.Hashes
	if hashesStr == "" {
		hashesStr = entry.VKHashes
	}
	var hashes []string
	for _, h := range strings.Split(hashesStr, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hashes = append(hashes, h)
		}
	}
	if len(hashes) == 0 {
		return ProfileData{}, fmt.Errorf("профиль %q: hashes обязателен", entry.Name)
	}

	workers := entry.Workers
	if workers <= 0 && entry.WorkersPerHash > 0 {
		workers = entry.WorkersPerHash * len(hashes)
	}
	if workers <= 0 {
		workers = defaultWorkers
	}

	listenPort := entry.ListenPort
	if listenPort <= 0 {
		listenPort = entry.Port
	}
	if listenPort <= 0 {
		listenPort = 9000
	}
	listen := fmt.Sprintf("127.0.0.1:%d", listenPort)

	prof := ProfileData{
		PeerAddr:     entry.Peer,
		Password:     password,
		Hashes:       hashes,
		Listen:       listen,
		Workers:      workers,
		DeviceID:     getOrCreateDeviceID(),
		Subscription: subName,
	}
	return prof, nil
}

func applySubscription(subName, url string) (created []string, removed []string, err error) {
	sub, err := loadSubscription(subName)
	if err != nil {
		sub = &SubscriptionConfig{
			URL:              url,
			SubscriptionName: subName,
		}
	} else if url != "" {
		sub.URL = url
	}

	data, err := fetchSubscriptionJSON(sub.URL)
	if err != nil {
		return nil, nil, err
	}

	parsed, err := parseSubscriptionJSON(data)
	if err != nil {
		return nil, nil, err
	}

	subName = sub.SubscriptionName

	for _, oldName := range sub.Profiles {
		if p, lerr := loadProfile(oldName); lerr == nil && p.Subscription == subName {
			if err := os.Remove(profilePath(oldName)); err == nil {
				removed = append(removed, oldName)
			}
		} else {
			if rerr := os.Remove(profilePath(oldName)); rerr == nil {
				removed = append(removed, oldName)
			}
		}
	}

	sub.SubscriptionName = subName
	sub.Description = parsed.Description
	sub.TrafficUsedMb = parsed.TrafficUsedMb
	sub.TrafficLimitMb = parsed.TrafficLimitMb
	sub.UpdatedAt = parsed.UpdatedAt
	sub.Version = parsed.Version
	sub.LastFetch = time.Now().Format(time.RFC3339)

	var newProfiles []string
	nameCounter := make(map[string]int)

	for _, entry := range parsed.Profiles {
		prof, err := subscriptionEntryToProfile(entry, subName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] %v\n", err)
			continue
		}

		baseName := normalizeImportName(entry.Name)
		if baseName == "" {
			baseName = subName
		}
		finalName := baseName

		existing, _ := loadProfile(finalName)
		if existing != nil && existing.Subscription != subName {
			for {
				c := nameCounter[baseName]
				nameCounter[baseName]++
				candidate := fmt.Sprintf("%s_sub%d", baseName, c)
				if _, err := loadProfile(candidate); err != nil {
					finalName = candidate
					break
				}
			}
		}

		if err := saveProfile(finalName, prof); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Не удалось сохранить профиль '%s': %v\n", finalName, err)
			continue
		}
		newProfiles = append(newProfiles, finalName)
		created = append(created, finalName)
	}

	sub.Profiles = newProfiles
	if err := saveSubscription(subName, sub); err != nil {
		return created, removed, fmt.Errorf("не удалось сохранить конфиг подписки: %w", err)
	}

	return created, removed, nil
}
