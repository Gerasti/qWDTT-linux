package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

	// Prevent editing read-only profiles
	if strings.HasPrefix(name, "ro-") {
		log.Fatalf("Cannot edit read-only profile '%s'. Read-only profiles can only be enabled/disabled.", name)
	}

	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	peer := fs.String("peer", "", "Адрес сервера (IP:PORT)")
	password := fs.String("password", "", "Пароль")
	hashes := fs.String("hashes", "", "VK-хеши через запятую")
	deviceID := fs.String("device-id", "", "Device ID")
	listen := fs.String("listen", "", "Локальный адрес")
	priority := fs.Int("priority", -1, "Приоритет (чем выше, тем раньше)")
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

	if *priority != -1 {
		prof.Priority = *priority
		changed = true
		fmt.Printf("[*] Приоритет изменён: %d\n", *priority)
	}

	if !changed {
		fmt.Println("[!] Не указаны параметры для изменения")
		fmt.Println("Используйте: -peer, -password, -hashes, -device-id, -listen или -priority")
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

	// Prevent removing read-only profiles
	if strings.HasPrefix(name, "ro-") {
		log.Fatalf("Cannot remove read-only profile '%s'. Read-only profiles are managed by system configuration.", name)
	}

	if err := os.Remove(profilePath(name)); err != nil {
		log.Fatalf("Ошибка удаления профиля: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' удалён\n", name)
}

func listCmd() {
	type profileInfo struct {
		name     string
		peer     string
		hashes   int
		status   string
		priority int
		active   bool
		readOnly bool
	}

	var regularProfiles []profileInfo
	var readOnlyProfiles []profileInfo
	maxNameLen := 0
	maxPeerLen := 0

	activeProfile := getActiveProfile()

	// ANSI color codes
	const (
		colorReset = "\033[0m"
		colorGreen = "\033[32m"
		colorRed   = "\033[31m"
	)

	// Read profiles from both directories
	readProfilesFromDir := func(dir string, isReadOnly bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			log.Fatalf("Ошибка чтения профилей: %v", err)
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".json")
			prof, err := loadProfile(name)
			if err != nil {
				continue
			}

			// Get status from status.json
			enabled := isProfileEnabled(name)
			status := "enabled"
			if !enabled {
				status = "disabled"
			}

			info := profileInfo{
				name:     name,
				peer:     prof.PeerAddr,
				hashes:   len(prof.Hashes),
				status:   status,
				priority: prof.Priority,
				active:   name == activeProfile,
				readOnly: isReadOnly,
			}

			if isReadOnly {
				readOnlyProfiles = append(readOnlyProfiles, info)
			} else {
				regularProfiles = append(regularProfiles, info)
			}

			if len(name) > maxNameLen {
				maxNameLen = len(name)
			}
			if len(prof.PeerAddr) > maxPeerLen {
				maxPeerLen = len(prof.PeerAddr)
			}
		}
	}

	// Read regular profiles
	readProfilesFromDir(filepath.Join(configDir(), "profiles"), false)

	// Read read-only profiles
	readProfilesFromDir(filepath.Join(configDir(), "ro-profiles"), true)

	if len(regularProfiles) == 0 && len(readOnlyProfiles) == 0 {
		fmt.Println("Нет сохранённых профилей")
		return
	}

	printProfiles := func(profiles []profileInfo, title string) {
		if len(profiles) == 0 {
			return
		}
		fmt.Printf("\n%s:\n", title)
		for _, p := range profiles {
			activeMarker := " "
			if p.active {
				activeMarker = "*"
			}

			// Pad status to fixed width BEFORE coloring
			paddedStatus := fmt.Sprintf("%-8s", p.status)

			// Color the padded status
			statusColor := colorGreen
			if p.status == "disabled" {
				statusColor = colorRed
			}
			coloredStatus := statusColor + paddedStatus + colorReset

			fmt.Printf(" %s %-*s  %-*s  %d хешей  [%s]  priority: %d\n",
				activeMarker,
				maxNameLen, p.name,
				maxPeerLen, p.peer,
				p.hashes,
				coloredStatus,
				p.priority)
		}
	}

	printProfiles(regularProfiles, "Профили")
	printProfiles(readOnlyProfiles, "Read-only профили")
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

	fmt.Printf("Профиль: %s", name)
	if strings.HasPrefix(name, "ro-") {
		fmt.Printf(" [read-only]")
	}
	fmt.Println()
	fmt.Printf("  Peer: %s\n", prof.PeerAddr)
	fmt.Printf("  Password: %s\n", maskPassword(prof.Password))
	fmt.Printf("  Listen: %s\n", prof.Listen)
	if prof.TurnHost != "" {
		fmt.Printf("  TURN: %s:%s\n", prof.TurnHost, prof.TurnPort)
	}
	if prof.DeviceID != "" {
		fmt.Printf("  Device ID: %s\n", prof.DeviceID)
	}

	// Get status from status.json
	enabled := isProfileEnabled(name)
	status := "enabled"
	if !enabled {
		status = "disabled"
	}
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Priority: %d\n", prof.Priority)
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

func enableCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli enable <name>\n")
		os.Exit(1)
	}

	name := os.Args[2]

	// Check if profile exists
	_, err := loadProfile(name)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	if isProfileEnabled(name) {
		fmt.Printf("[*] Профиль '%s' уже включен\n", name)
		return
	}

	if err := setProfileEnabled(name, true); err != nil {
		log.Fatalf("Ошибка изменения статуса: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' включен\n", name)
}

func disableCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt-cli disable <name>\n")
		os.Exit(1)
	}

	name := os.Args[2]

	// Check if profile exists
	_, err := loadProfile(name)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	if !isProfileEnabled(name) {
		fmt.Printf("[*] Профиль '%s' уже отключен\n", name)
		return
	}

	if err := setProfileEnabled(name, false); err != nil {
		log.Fatalf("Ошибка изменения статуса: %v", err)
	}

	fmt.Printf("[OK] Профиль '%s' отключен\n", name)
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

func profilesDir() string {
	return filepath.Join(configDir(), "profiles")
}

func listProfileNames() []string {
	type profileWithPriority struct {
		name     string
		priority int
	}

	var profiles []profileWithPriority

	// Read from both directories
	readFromDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".json")
			prof, err := loadProfile(name)
			if err != nil {
				continue
			}
			if isProfileEnabled(name) {
				profiles = append(profiles, profileWithPriority{
					name:     name,
					priority: prof.Priority,
				})
			}
		}
	}

	readFromDir(filepath.Join(configDir(), "profiles"))
	readFromDir(filepath.Join(configDir(), "ro-profiles"))

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].priority > profiles[j].priority
	})

	var names []string
	for _, p := range profiles {
		names = append(names, p.name)
	}
	return names
}

func listAllProfileNames() []string {
	var names []string

	readFromDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}

	readFromDir(filepath.Join(configDir(), "profiles"))
	readFromDir(filepath.Join(configDir(), "ro-profiles"))

	return names
}

func listDisabledProfileNames() []string {
	var names []string

	readFromDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".json")
			if !isProfileEnabled(name) {
				names = append(names, name)
			}
		}
	}

	readFromDir(filepath.Join(configDir(), "profiles"))
	readFromDir(filepath.Join(configDir(), "ro-profiles"))

	return names
}

func disconnectCmd() {
	activeProfile := getActiveProfile()
	if activeProfile == "" {
		fmt.Println("[!] Нет активного подключения")
		os.Exit(1)
	}

	fmt.Printf("[*] Отключение профиля '%s'...\n", activeProfile)

	cmd := exec.Command("pgrep", "-f", "qwdtt-cli")
	output, err := cmd.Output()
	if err == nil {
		pids := strings.Split(strings.TrimSpace(string(output)), "\n")
		selfPID := fmt.Sprintf("%d", os.Getpid())

		for _, pid := range pids {
			pid = strings.TrimSpace(pid)
			if pid == "" || pid == selfPID {
				continue
			}

			fmt.Printf("[*] Завершение процесса qwdtt-cli (PID: %s)...\n", pid)
			killCmd := exec.Command("kill", "-INT", pid)
			killCmd.Run()
		}

		time.Sleep(2 * time.Second)

		for _, pid := range pids {
			pid = strings.TrimSpace(pid)
			if pid == "" || pid == selfPID {
				continue
			}

			if exec.Command("kill", "-0", pid).Run() == nil {
				fmt.Printf("[*] Принудительное завершение PID: %s...\n", pid)
				exec.Command("kill", "-9", pid).Run()
			}
		}
	}

	if err := teardownWG(); err == nil {
		fmt.Println("[OK] WireGuard интерфейс удален")
	}

	clearActiveProfile()
	fmt.Println("[OK] Отключено")
}

func debugCmd() {
	activeProfile := getActiveProfile()
	if activeProfile == "" {
		fmt.Println("[!] Нет активного подключения")
		os.Exit(1)
	}

	fmt.Printf("=== DEBUG INFO ===\n\n")
	fmt.Printf("Активный профиль: %s\n\n", activeProfile)

	prof, err := loadProfile(activeProfile)
	if err != nil {
		fmt.Printf("[ERROR] Не удалось загрузить профиль: %v\n", err)
	} else {
		fmt.Printf("Конфигурация профиля:\n")
		fmt.Printf("  Peer: %s\n", prof.PeerAddr)
		fmt.Printf("  Listen: %s\n", prof.Listen)
		if prof.TurnHost != "" {
			fmt.Printf("  TURN: %s:%s\n", prof.TurnHost, prof.TurnPort)
		}
		fmt.Printf("  Device ID: %s\n", prof.DeviceID)
		fmt.Printf("  Priority: %d\n\n", prof.Priority)
	}

	if stats, err := getWGStats(); err == nil {
		fmt.Printf("Input:\n")
		fmt.Printf("  Bytes: %s\n", formatBytes(stats.RxBytes))
		fmt.Printf("  Packets: %d\n\n", stats.RxPackets)

		fmt.Printf("Output:\n")
		fmt.Printf("  Bytes: %s\n", formatBytes(stats.TxBytes))
		fmt.Printf("  Packets: %d\n\n", stats.TxPackets)
	} else {
		fmt.Printf("Input/Output: [ERROR] %v\n\n", err)
	}

	fmt.Printf("Использование ресурсов (qwdtt-cli):\n")
	if usage, err := getProcessUsage(); err == nil {
		fmt.Printf("  CPU: %.1f%%\n", usage.CPU)
		fmt.Printf("  RAM: %s\n", formatBytes(usage.Memory))
	} else {
		fmt.Printf("  [ERROR] %v\n", err)
	}
}
