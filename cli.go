package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"wg-turn-client/core"
)

func printUsage() {
	fmt.Printf(`qwdtt-cli v%s - VPN client через TURN-серверы VK

Использование:
  qwdtt-cli connect <profile> [флаги]  Подключиться к VPN
  qwdtt-cli add <name> <wdtt://...>    Добавить профиль
  qwdtt-cli edit <name> [флаги]        Редактировать профиль
  qwdtt-cli remove <name>              Удалить профиль
  qwdtt-cli list                       Список профилей
  qwdtt-cli show <name>                Показать профиль
  qwdtt-cli device-id [id]             Показать/установить Device ID
  qwdtt-cli regenerate-id              Перегенерировать Device ID
  qwdtt-cli version                    Версия

Короткие команды:
  con  - connect
  sh   - show
  ls   - list
  rm   - remove
  id   - device-id

Флаги connect:
  -workers N       Количество воркеров, кратно 9 (9, 18, 27, ..., default: 9)
  -mtu N           MTU туннеля (default: 1280, max: 1500)
  -hashes H1,H2    Переопределить VK-хеши профиля

Флаги edit:
  -peer ADDR       Изменить адрес сервера (IP:PORT)
  -password PASS   Изменить пароль
  -hashes H1,H2    Изменить VK-хеши
  -device-id ID    Изменить Device ID
  -listen ADDR     Изменить локальный UDP адрес для туннеля (default: 127.0.0.1:9000)

Примеры:
  qwdtt-cli add myserver wdtt://1.2.3.4:56000:56001:0:pass:hash1,hash2#MyServer
  qwdtt-cli edit myserver -password newpass
  qwdtt-cli edit myserver -device-id 0fd4ffcddb764351
  qwdtt-cli con myserver
  qwdtt-cli con myserver -workers 18
  qwdtt-cli sh myserver
  qwdtt-cli id                         # показать текущий Device ID
  qwdtt-cli id 0fd4ffcddb764351        # установить Device ID
`, version)
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	dir := filepath.Join(base, "qwdtt")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func profilePath(name string) string {
	return filepath.Join(configDir(), "profiles", name+".json")
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

type ProfileData struct {
	PeerAddr string   `json:"peer"`
	Password string   `json:"password"`
	Hashes   []string `json:"hashes"`
	Listen   string   `json:"listen,omitempty"`
	TurnHost string   `json:"turn,omitempty"`
	TurnPort string   `json:"port,omitempty"`
	DeviceID string   `json:"device_id,omitempty"`
}

func connectCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli connect <profile> [flags]\n")
		os.Exit(1)
	}

	profileName := os.Args[2]

	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	workers := fs.Int("workers", 9, "Количество воркеров")
	mtu := fs.Int("mtu", 1280, "MTU туннеля")
	hashes := fs.String("hashes", "", "VK-хеши через запятую")
	fs.Parse(os.Args[3:])

	prof, err := loadProfile(profileName)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	deviceID := prof.DeviceID
	if deviceID == "" {
		deviceID = getOrCreateDeviceID()
	}

	cfg := core.Config{
		PeerAddr:    prof.PeerAddr,
		Password:    prof.Password,
		Hashes:      prof.Hashes,
		Listen:      prof.Listen,
		TurnHost:    prof.TurnHost,
		TurnPort:    prof.TurnPort,
		DeviceID:    deviceID,
		Workers:     *workers,
		CaptchaMode: "rjs",
		MTU:         *mtu,
	}

	if *hashes != "" {
		cfg.Hashes = strings.Split(*hashes, ",")
		for i := range cfg.Hashes {
			cfg.Hashes[i] = strings.TrimSpace(cfg.Hashes[i])
		}
	}

	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}

	fmt.Printf("Подключение к профилю '%s'...\n", profileName)
	fmt.Printf("  Peer: %s\n", cfg.PeerAddr)
	fmt.Printf("  Workers: %d\n", cfg.Workers)

	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		log.Fatalf("Ошибка запуска: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nОтключение...")
		c.Stop()
	}()

	connected := false
	wgConfigured := false
	var coreInstance *core.Core
	coreInstance = c

	for ev := range events {
		switch ev.Type {
		case core.EventState:
			switch ev.Status {
			case "connecting":
				fmt.Println("[*] Подключение...")
			case "running":
				if !connected {
					connected = true
					fmt.Println("[OK] Туннель активен")
				}
			case "disconnected":
				fmt.Println("[*] Отключено")
			}
		case core.EventLog:
			if strings.Contains(ev.Message, "Конфиг получен") {
				fmt.Printf("[OK] %s\n", ev.Message)
			} else if ev.Level == "ERROR" {
				fmt.Printf("[ERROR] %s\n", ev.Message)
			} else if strings.Contains(ev.Message, "FATAL") {
				fmt.Printf("[!] %s\n", ev.Message)
			}
		case core.EventError:
			fmt.Printf("[ERROR] %s\n", ev.Message)
		case core.EventEvent:
			if ev.Name == "wg_config" && !wgConfigured {
				wgConfigured = true
				fmt.Printf("[*] WireGuard конфиг получен (%d байт)\n", len(ev.Data))
				turnIPs := coreInstance.GetTurnIPs()
				fmt.Printf("[*] Настройка интерфейса wg-qwdtt...\n")
				if err := applyWGConfig(ev.Data, turnIPs); err != nil {
					fmt.Printf("[ERROR] Ошибка настройки WireGuard: %v\n", err)
					fmt.Println("  Убедитесь, что:")
					fmt.Println("  1. Команды ip и wg доступны")
					fmt.Println("  2. В /etc/sudoers добавлено: your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg")
				} else {
					fmt.Println("[OK] WireGuard интерфейс настроен и активен")
					fmt.Println("[*] Весь трафик теперь идет через VPN")
				}
			} else if ev.Name == "captcha_required" {
				parts := strings.Split(ev.Data, "|")
				if len(parts) >= 1 {
					fmt.Printf("[!] Требуется капча (режим: %s)\n", parts[0])
					fmt.Println("  CLI ещё не поддерживает интерактивное решение капчи")
					fmt.Println("  Можете сделать issue на github.com/Gerasti/qWDTT-linux")
				}
			}
		case core.EventStats:
			if connected && ev.RxBytes > 0 {
				fmt.Printf("\r[STATS] RX: %s | TX: %s | Workers: %d   ",
					formatBytes(ev.RxBytes),
					formatBytes(ev.TxBytes),
					ev.Workers)
			}
		}
	}

	if wgConfigured {
		fmt.Println("\n[*] Удаление WireGuard интерфейса...")
		if err := teardownWG(); err != nil {
			fmt.Printf("[!] Ошибка при удалении интерфейса: %v\n", err)
		}
	}

	fmt.Println("[*] Завершено")
}

func addCmd() {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	deviceID := fs.String("device-id", "", "Device ID (например, 0fd4ffcddb759420)")
	fs.Parse(os.Args[3:])

	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli add <name> <wdtt://...> [-device-id ID]\n")
		os.Exit(1)
	}

	name := os.Args[2]
	url := fs.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli add <name> <wdtt://...> [-device-id ID]\n")
		os.Exit(1)
	}

	link, err := parseWdttURL(url)
	if err != nil {
		log.Fatalf("Ошибка парсинга URL: %v", err)
	}

	devID := *deviceID
	if devID == "" {
		devID = getOrCreateDeviceID()
	}

	prof := ProfileData{
		PeerAddr: fmt.Sprintf("%s:%s", link.IP, link.DTLSPort),
		Password: link.Password,
		Hashes:   link.Hashes,
		Listen:   "127.0.0.1:9000",
		DeviceID: devID,
	}

	if err := saveProfile(name, prof); err != nil {
		log.Fatalf("Ошибка сохранения профиля: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' добавлен\n", name)
	if link.Name != "" && link.Name != "Server" {
		fmt.Printf("  Название: %s\n", link.Name)
	}
	fmt.Printf("  Peer: %s\n", prof.PeerAddr)
	fmt.Printf("  Хешей: %d\n", len(prof.Hashes))
}

func editCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli edit <name> [флаги]\n")
		os.Exit(1)
	}

	name := os.Args[2]

	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	peer := fs.String("peer", "", "Адрес сервера (IP:PORT)")
	password := fs.String("password", "", "Пароль")
	hashes := fs.String("hashes", "", "VK-хеши через запятую")
	deviceID := fs.String("device-id", "", "Device ID")
	listen := fs.String("listen", "", "Локальный адрес")
	fs.Parse(os.Args[3:])

	prof, err := loadProfile(name)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	changed := false

	if *peer != "" {
		prof.PeerAddr = *peer
		changed = true
		fmt.Printf("[*] Peer изменён: %s\n", *peer)
	}

	if *password != "" {
		prof.Password = *password
		changed = true
		fmt.Println("[*] Пароль изменён")
	}

	if *hashes != "" {
		prof.Hashes = nil
		for _, h := range strings.Split(*hashes, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				prof.Hashes = append(prof.Hashes, h)
			}
		}
		changed = true
		fmt.Printf("[*] Хеши изменены (%d шт.)\n", len(prof.Hashes))
	}

	if *deviceID != "" {
		prof.DeviceID = *deviceID
		changed = true
		fmt.Printf("[*] Device ID изменён: %s\n", *deviceID)
	}

	if *listen != "" {
		prof.Listen = *listen
		changed = true
		fmt.Printf("[*] Listen изменён: %s\n", *listen)
	}

	if !changed {
		fmt.Println("[!] Не указаны параметры для изменения")
		fmt.Println("Используйте: -peer, -password, -hashes, -device-id или -listen")
		os.Exit(1)
	}

	if err := saveProfile(name, *prof); err != nil {
		log.Fatalf("Ошибка сохранения профиля: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' обновлён\n", name)
}

func removeCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli remove <name>\n")
		os.Exit(1)
	}

	name := os.Args[2]
	if err := os.Remove(profilePath(name)); err != nil {
		log.Fatalf("Ошибка удаления профиля: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' удалён\n", name)
}

func listCmd() {
	dir := filepath.Join(configDir(), "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Нет сохранённых профилей")
			return
		}
		log.Fatalf("Ошибка чтения профилей: %v", err)
	}

	if len(entries) == 0 {
		fmt.Println("Нет сохранённых профилей")
		return
	}

	fmt.Println("Профили:")
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		prof, err := loadProfile(name)
		if err != nil {
			continue
		}
		fmt.Printf("  - %s - %s (%d хешей)\n", name, prof.PeerAddr, len(prof.Hashes))
	}
}

func showCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli show <name>\n")
		os.Exit(1)
	}

	name := os.Args[2]
	prof, err := loadProfile(name)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	fmt.Printf("Профиль: %s\n", name)
	fmt.Printf("  Peer: %s\n", prof.PeerAddr)
	fmt.Printf("  Password: %s\n", maskPassword(prof.Password))
	fmt.Printf("  Listen: %s\n", prof.Listen)
	if prof.TurnHost != "" {
		fmt.Printf("  TURN: %s:%s\n", prof.TurnHost, prof.TurnPort)
	}
	if prof.DeviceID != "" {
		fmt.Printf("  Device ID: %s\n", prof.DeviceID)
	}
	fmt.Printf("  Хеши (%d):\n", len(prof.Hashes))
	for i, h := range prof.Hashes {
		fmt.Printf("    %d. %s\n", i+1, h)
	}
}

func regenerateIDCmd() {
	oldID := ""
	if data, err := os.ReadFile(deviceIDPath()); err == nil {
		oldID = strings.TrimSpace(string(data))
	}

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Ошибка генерации Device ID: %v", err)
	}
	newID := hex.EncodeToString(b)

	if err := os.WriteFile(deviceIDPath(), []byte(newID), 0o600); err != nil {
		log.Fatalf("Ошибка сохранения Device ID: %v", err)
	}

	if oldID != "" {
		fmt.Printf("[*] Старый Device ID: %s\n", oldID)
	}
	fmt.Printf("[OK] Новый Device ID: %s\n", newID)
	fmt.Println("[*] Device ID перегенерирован успешно")
}

func deviceIDCmd() {
	if len(os.Args) < 3 {
		if data, err := os.ReadFile(deviceIDPath()); err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				fmt.Printf("Текущий Device ID: %s\n", id)
				return
			}
		}
		fmt.Println("Device ID не установлен")
		fmt.Println("Использование: qwdtt-cli device-id <16-символьный-hex-ID>")
		os.Exit(1)
	}

	newID := strings.TrimSpace(os.Args[2])

	if len(newID) != 16 {
		log.Fatalf("Ошибка: Device ID должен быть ровно 16 символов (получено %d)", len(newID))
	}

	for _, c := range newID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			log.Fatalf("Ошибка: Device ID должен содержать только hex-символы (0-9, a-f)")
		}
	}

	newID = strings.ToLower(newID)

	oldID := ""
	if data, err := os.ReadFile(deviceIDPath()); err == nil {
		oldID = strings.TrimSpace(string(data))
	}

	if err := os.WriteFile(deviceIDPath(), []byte(newID), 0o600); err != nil {
		log.Fatalf("Ошибка сохранения Device ID: %v", err)
	}

	if oldID != "" && oldID != newID {
		fmt.Printf("[*] Старый Device ID: %s\n", oldID)
	}
	fmt.Printf("[OK] Новый Device ID: %s\n", newID)
	fmt.Println("[*] Device ID установлен успешно")
}

type WdttLink struct {
	IP       string
	DTLSPort string
	Password string
	Hashes   []string
	Name     string
}

func parseWdttURL(raw string) (*WdttLink, error) {
	stripped := strings.TrimPrefix(strings.TrimSpace(raw), "wdtt://")
	parts := strings.Split(stripped, ":")
	if len(parts) < 5 {
		return nil, fmt.Errorf("неверный формат URL (нужно минимум 5 полей)")
	}

	ip := parts[0]
	dtlsPort := parts[1]
	tail := strings.Join(parts[4:], ":")

	name := "Server"
	hashIdx := strings.LastIndex(tail, "#")
	passwordAndHashes := tail
	if hashIdx != -1 {
		candidate := strings.TrimSpace(tail[hashIdx+1:])
		if candidate != "" {
			name = candidate
		}
		passwordAndHashes = tail[:hashIdx]
	}

	colonIdx := strings.LastIndex(passwordAndHashes, ":")
	var password string
	var hashes []string
	if colonIdx != -1 {
		password = passwordAndHashes[:colonIdx]
		hashStr := passwordAndHashes[colonIdx+1:]
		for _, h := range strings.Split(hashStr, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				hashes = append(hashes, h)
			}
		}
	} else {
		password = passwordAndHashes
	}

	if ip == "" || dtlsPort == "" || password == "" {
		return nil, fmt.Errorf("не указаны обязательные поля")
	}

	return &WdttLink{
		IP:       ip,
		DTLSPort: dtlsPort,
		Password: password,
		Hashes:   hashes,
		Name:     name,
	}, nil
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

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
