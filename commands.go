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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal"
)

func addCmd() {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	deviceID := fs.String("device-id", "", "Device ID (например, 0fd4ffcddb759420)")
	fs.Parse(os.Args[3:])

	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt add <name> <wdtt://...> [-device-id ID]\n")
		os.Exit(1)
	}

	name := os.Args[2]
	url := fs.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt add <name> <wdtt://...> [-device-id ID]\n")
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
		fmt.Fprintf(os.Stderr, "Usage: qwdtt edit <name> [флаги]\n")
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
		fmt.Fprintf(os.Stderr, "Usage: qwdtt remove <name>\n")
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
		name      string
		peer      string
		hashes    int
		status    string
		priority  int
		active    bool
		mode      string // "tun" or "socks" (only set if active)
		socksPort int    // SOCKS5 port (only set if active)
		readOnly  bool
	}

	var regularProfiles []profileInfo
	var readOnlyProfiles []profileInfo
	maxNameLen := 0
	maxPeerLen := 0

	// ANSI color codes
	const (
		colorReset  = "\033[0m"
		colorGreen  = "\033[32m"
		colorRed    = "\033[31m"
		colorCyan   = "\033[36m"
		colorYellow = "\033[33m"
	)

	// Get list of currently running profiles with details (mode, socks port)
	runningDetails := getRunningProfileDetails()

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

			active := false
			mode := ""
			socksPort := 0
			if d, ok := runningDetails[name]; ok {
				active = true
				mode = d.Mode
				socksPort = d.SocksPort
			}

			info := profileInfo{
				name:      name,
				peer:      prof.PeerAddr,
				hashes:    len(prof.Hashes),
				status:    status,
				priority:  prof.Priority,
				active:    active,
				mode:      mode,
				socksPort: socksPort,
				readOnly:  isReadOnly,
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

	// Sort profiles by priority (highest first), then by name
	sortProfilesByPriority := func(profiles []profileInfo) {
		sort.Slice(profiles, func(i, j int) bool {
			if profiles[i].priority != profiles[j].priority {
				return profiles[i].priority > profiles[j].priority
			}
			return profiles[i].name < profiles[j].name
		})
	}
	sortProfilesByPriority(regularProfiles)
	sortProfilesByPriority(readOnlyProfiles)

	if len(regularProfiles) == 0 && len(readOnlyProfiles) == 0 {
		fmt.Println("Нет сохранённых профилей")
		return
	}

	// Show autoswitch status with mode info
	currentAutoswitchProfile := getAutoswitchCurrentProfile()

	printProfiles := func(profiles []profileInfo, title string) {
		if len(profiles) == 0 {
			return
		}
		fmt.Printf("\n%s:\n", title)
		for _, p := range profiles {
			activeMarker := " "
			modeStr := ""
			if p.active {
				activeMarker = "*"
				if p.mode == "socks" {
					modeStr = fmt.Sprintf(" [socks:%d]", p.socksPort)
				} else {
					modeStr = " [tun]"
				}
			}

			// Mark current autoswitch profile
			isAutoswitchCurrent := currentAutoswitchProfile == p.name

			// For non-active autoswitch current profile, show marker in mode field
			// For active autoswitch current, show marker after mode field
			autoswitchMarker := ""
			if isAutoswitchCurrent {
				if p.active {
					autoswitchMarker = colorYellow + " [autoswitch-active]" + colorReset
				} else {
					modeStr = " [autoswitch-active]"
				}
			}

			// Pad status to fixed width BEFORE coloring
			paddedStatus := fmt.Sprintf("%-8s", p.status)

			// Color the padded status
			statusColor := colorGreen
			if p.status == "disabled" {
				statusColor = colorRed
			}
			coloredStatus := statusColor + paddedStatus + colorReset

			// Color and pad mode string for alignment
			paddedMode := fmt.Sprintf("%-22s", modeStr)
			coloredPaddedMode := paddedMode
			if p.active && p.mode == "socks" {
				coloredPaddedMode = colorCyan + paddedMode + colorReset
			}
			if isAutoswitchCurrent && !p.active {
				coloredPaddedMode = colorYellow + paddedMode + colorReset
			}

			fmt.Printf(" %s %-*s  %-*s  %d хешей  [%s]  priority: %-3d%s%s\n",
				activeMarker,
				maxNameLen, p.name,
				maxPeerLen, p.peer,
				p.hashes,
				coloredStatus,
				p.priority,
				coloredPaddedMode,
				autoswitchMarker)
		}
	}

	printProfiles(regularProfiles, "Профили")
	printProfiles(readOnlyProfiles, "Read-only профили")

	// Show autoswitch status with mode info
	if d, ok := runningDetails["autoswitch"]; ok {
		modeStr := d.Mode
		portStr := ""
		if d.Mode == "socks" {
			portStr = fmt.Sprintf(":%d", d.SocksPort)
		}
		if currentAutoswitchProfile != "" {
			fmt.Printf("\n[*] Режим авто-переключения активен (%s%s, PID: qwdtt-autoswitch)\n", modeStr, portStr)
			fmt.Printf("[*] Текущий профиль autoswitch: %s%s%s\n", colorYellow, currentAutoswitchProfile, colorReset)
		} else {
			fmt.Printf("\n[*] Режим авто-переключения активен (%s%s, PID: qwdtt-autoswitch)\n", modeStr, portStr)
			fmt.Println("[*] Текущий профиль autoswitch: (нет активного подключения)")
		}
	} else if getActiveProfile() == "autoswitch" {
		fmt.Println("\n[*] Режим авто-переключения активен (PID: qwdtt-autoswitch)")
		if currentAutoswitchProfile != "" {
			fmt.Printf("[*] Текущий профиль autoswitch: %s%s%s\n", colorYellow, currentAutoswitchProfile, colorReset)
		} else {
			fmt.Println("[*] Текущий профиль autoswitch: (нет активного подключения)")
		}
	}
}

func showCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt show <name>\n")
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

	// Show runtime mode (tun/socks) if the profile is currently running
	details := getRunningProfileDetails()
	if d, ok := details[name]; ok {
		fmt.Printf("  Режим: %s\n", d.Mode)
		if d.Mode == "socks" {
			fmt.Printf("  SOCKS5 порт: %d\n", d.SocksPort)
		}
		fmt.Printf("  PID: %d\n", d.PID)
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

func enableCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt enable <name>\n")
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
		fmt.Fprintf(os.Stderr, "Usage: qwdtt disable <name>\n")
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
		fmt.Println("Использование: qwdtt device-id <16-символьный-hex-ID>")
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

// Get list of currently running profiles by checking process command lines
func getRunningProfiles() map[string]bool {
	running := make(map[string]bool)

	// First try pidfile-based discovery
	entries, err := os.ReadDir(pidFilesDir())
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".pid") {
				continue
			}
			if !strings.HasPrefix(name, "qwdtt-") {
				continue
			}
			// Extract profile name: qwdtt-<profile>.pid
			profile := strings.TrimSuffix(name, ".pid")
			profile = strings.TrimPrefix(profile, "qwdtt-")

			if profile == "" {
				continue
			}

			pid, err := strconv.Atoi(strings.TrimSpace(readPidFile(pidFilePath(profile))))
			if err != nil {
				continue
			}

			if isProcessAlive(pid) {
				running[profile] = true
			}
		}
		if len(running) > 0 {
			return running
		}
	}

	// Fallback: pgrep + /proc/<pid>/cmdline
	cmd := exec.Command("pgrep", "-f", "qwdtt")
	output, err := cmd.Output()
	if err != nil {
		return running
	}

	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pidStr := range pids {
		pidStr = strings.TrimSpace(pidStr)
		if pidStr == "" {
			continue
		}

		cmdlinePath := fmt.Sprintf("/proc/%s/cmdline", pidStr)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
		fields := strings.Fields(cmdline)
		for i, field := range fields {
			if field == "con" && i+1 < len(fields) {
				profile := fields[i+1]
				if !strings.HasPrefix(profile, "-") {
					running[profile] = true
				}
			}
			if strings.Contains(field, "con ") {
				parts := strings.Split(field, " ")
				for _, part := range parts {
					if part != "con" && !strings.HasPrefix(part, "-") && part != "" {
						running[part] = true
					}
				}
			}
		}
	}

	return running
}

// readPidFile reads the content of a pid file, returning empty string on error.
func readPidFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Check if a profile is currently running
func isProfileRunning(profile string, runningProfiles map[string]bool) bool {
	return runningProfiles[profile]
}

func disconnectCmd() {
	var targetProfile string
	if len(os.Args) >= 3 {
		targetProfile = os.Args[2]
	}

	// Collect all running profiles
	runningDetails := getRunningProfileDetails()
	running := make([]string, 0, len(runningDetails))
	for name := range runningDetails {
		if name == "autoswitch" {
			continue
		}
		running = append(running, name)
	}
	// Also check for autoswitch daemon
	if isDaemonRunning("autoswitch") {
		running = append(running, "autoswitch")
	}

	// If no explicit target, try active profile first
	if targetProfile == "" {
		targetProfile = getActiveProfile()
	}

	// If still no target, or if there are multiple running profiles,
	// show interactive selection
	if targetProfile == "" || len(running) > 1 {
		if len(running) == 0 {
			fmt.Println("[!] Нет активного подключения и профиль не указан")
			os.Exit(1)
		}

		// Sort: tun profiles first, then socks, then autoswitch
		sort.Slice(running, func(i, j int) bool {
			di, oki := runningDetails[running[i]]
			dj, okj := runningDetails[running[j]]
			mi := "tun"
			if oki {
				mi = di.Mode
			}
			mj := "tun"
			if okj {
				mj = dj.Mode
			}
			if mi == "tun" && mj == "socks" {
				return true
			}
			if mi == "socks" && mj == "tun" {
				return false
			}
			return running[i] < running[j]
		})

		if len(running) == 1 && targetProfile == running[0] {
			// Only one running profile and it matches active profile — use it
		} else if len(running) == 1 {
			targetProfile = running[0]
		} else {
			// Multiple running — interactive selection
			fmt.Println("Активные подключения:")
			for i, name := range running {
				var detailStr string
				if d, ok := runningDetails[name]; ok {
					if d.Mode == "socks" {
						detailStr = fmt.Sprintf(" [socks:%d]", d.SocksPort)
					} else {
						detailStr = " [tun]"
					}
				} else if name == "autoswitch" {
					detailStr = " [autoswitch]"
				}
				fmt.Printf("  %d. %s%s\n", i+1, name, detailStr)
			}
			fmt.Print("> ")
			var choice int
			if _, err := fmt.Scanf("%d", &choice); err != nil || choice < 1 || choice > len(running) {
				fmt.Println("[!] Неверный выбор")
				os.Exit(1)
			}
			targetProfile = running[choice-1]
		}
	}

	// If autoswitch daemon is running, switch to the next profile instead of killing
	if isDaemonRunning("autoswitch") && targetProfile != "" && targetProfile != "autoswitch" {
		autoswitchPID, err := readPidForProfile("autoswitch")
		if err == nil && autoswitchPID > 0 {
			currentProfile := getAutoswitchCurrentProfile()
			if currentProfile == targetProfile {
				fmt.Printf("[*] Отключение текущего профиля '%s' от autoswitch (переключение на следующий)...\n", targetProfile)
			} else {
				fmt.Printf("[*] Сигнал переключения отправлен в autoswitch daemon (PID: %d)...\n", autoswitchPID)
			}
			syscall.Kill(autoswitchPID, syscall.SIGUSR1)
			clearAutoswitchCurrentProfile()
			notifyDisconnectedSync(targetProfile)
			fmt.Println("[OK] Отключено")
			return
		}
	}

	fmt.Printf("[*] Отключение профиля '%s'...\n", targetProfile)

	wasActive := false
	activeProfile := getActiveProfile()
	if activeProfile == targetProfile {
		wasActive = true
	}

	if !killByPidFile(targetProfile) {
		fmt.Printf("[*] Pid файл не найден, fallback на pgrep...\n")
		killByPgrep(targetProfile)
	}

	// Notify that the profile was disconnected
	notifyDisconnectedSync(targetProfile)

	// If disconnecting the active profile, clear it
	if wasActive {
		if err := teardownWG(); err == nil {
			fmt.Println("[OK] WireGuard конфиг удален")
		}
		clearActiveProfile()
	}

	fmt.Println("[OK] Отключено")
}

// killByPidFile sends SIGINT (then SIGTERM/SIGKILL) to the daemon process
// identified by the pid file for the given profile. Returns false if no
// valid pid file was found.
func killByPidFile(profile string) bool {
	pid, err := readPidForProfile(profile)
	if err != nil {
		return false
	}
	if pid == 0 {
		return false
	}
	return killDaemonProcess(pid, profile)
}

// readPidForProfile reads PID from pidfile.
func readPidForProfile(profile string) (int, error) {
	path := pidFilePath(profile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid in %s: %w", path, err)
	}
	return pid, nil
}

// killDaemonProcess sends SIGINT, waits up to 2s, then SIGKILL.
func killDaemonProcess(pid int, profile string) bool {
	if pid == os.Getpid() {
		return false
	}

	fmt.Printf("[*] Завершение процесса qwdtt (PID: %d) для профиля '%s'...\n", pid, profile)
	defer cleanupPidFile(profile)

	// Graceful SIGINT
	_ = syscall.Kill(pid, syscall.SIGINT)
	time.Sleep(2 * time.Second)

	// Force kill if still alive
	if isProcessAlive(pid) {
		fmt.Printf("[*] Принудительное завершение PID: %d...\n", pid)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return true
}

// killByPgrep fallback: original pgrep + /proc/<pid>/cmdline approach.
func killByPgrep(targetProfile string) {
	cmd := exec.Command("pgrep", "-f", "qwdtt")
	output, err := cmd.Output()
	if err != nil {
		return
	}
	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	selfPID := fmt.Sprintf("%d", os.Getpid())

	for _, pid := range pids {
		pid = strings.TrimSpace(pid)
		if pid == "" || pid == selfPID {
			continue
		}
		cmdlinePath := fmt.Sprintf("/proc/%s/cmdline", pid)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}
		if strings.Contains(string(cmdlineData), targetProfile) {
			fmt.Printf("[*] Завершение процесса qwdtt (PID: %s) для профиля '%s'...\n", pid, targetProfile)
			exec.Command("kill", "-INT", pid).Run()
		}
	}

	time.Sleep(2 * time.Second)

	for _, pid := range pids {
		pid = strings.TrimSpace(pid)
		if pid == "" || pid == selfPID {
			continue
		}
		cmdlinePath := fmt.Sprintf("/proc/%s/cmdline", pid)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}
		if strings.Contains(string(cmdlineData), targetProfile) {
			if exec.Command("kill", "-0", pid).Run() == nil {
				fmt.Printf("[*] Принудительное завершение PID: %s...\n", pid)
				exec.Command("kill", "-9", pid).Run()
			}
		}
	}
}

func debugCmd() {
	activeProfile := getActiveProfile()
	if activeProfile == "" {
		fmt.Println("[!] Нет активного подключения")
		os.Exit(1)
	}

	fmt.Printf("=== DEBUG INFO ===\n\n")

	// Show autoswitch status if active
	if activeProfile == "autoswitch" {
		fmt.Println("[*] Режим авто-переключения активен")
	}

	// Show all running profiles with details
	details := getRunningProfileDetails()
	if len(details) > 0 {
		// Sort: tun first, then socks, then autoswitch
		type profileEntry struct {
			name string
			d    *ProfileDetails
		}
		var sorted []profileEntry
		for name, d := range details {
			sorted = append(sorted, profileEntry{name, d})
		}
		sort.Slice(sorted, func(i, j int) bool {
			mi, mj := sorted[i].d.Mode, sorted[j].d.Mode
			if mi == "tun" && mj != "tun" {
				return true
			}
			if mj == "tun" && mi != "tun" {
				return false
			}
			return sorted[i].name < sorted[j].name
		})

		fmt.Printf("Активные профили (%d):\n", len(sorted))
		for _, e := range sorted {
			modeStr := e.d.Mode
			portStr := ""
			if e.d.Mode == "socks" {
				portStr = fmt.Sprintf(":%d", e.d.SocksPort)
			}
			fmt.Printf("  %s (%s%s, PID: %d)\n", e.name, modeStr, portStr, e.d.PID)
		}
		fmt.Println()

		// Show detailed config for each running profile
		for idx, e := range sorted {
			if idx > 0 {
				fmt.Println("  ---")
			}
			if e.name == "autoswitch" {
				// autoswitch is not a real profile — just show mode info
				fmt.Printf("Режим авто-переключения (autoswitch):\n")
				fmt.Printf("  Mode: %s\n", e.d.Mode)
				if e.d.Mode == "socks" {
					fmt.Printf("  SOCKS5 порт: %d\n", e.d.SocksPort)
				}
				fmt.Printf("  PID: %d\n", e.d.PID)

				// Show current autoswitch profile
				currentProfile := getAutoswitchCurrentProfile()
				if currentProfile != "" {
					fmt.Printf("  Текущий профиль: %s\n", currentProfile)
				} else {
					fmt.Printf("  Текущий профиль: (нет активного подключения)\n")
				}

				// Per-profile resource usage
				if usage, err := getProcessUsageByPID(e.d.PID); err == nil {
					fmt.Printf("  Активных воркеров: %d\n", usage.Workers)
				} else {
					fmt.Printf("  [ERROR] не удалось получить статистику: %v\n", err)
				}
				fmt.Println()
				continue
			}
			prof, err := loadProfile(e.name)
			if err != nil {
				fmt.Printf("[ERROR] Не удалось загрузить профиль '%s': %v\n", e.name, err)
				continue
			}
			fmt.Printf("Конфигурация профиля '%s':\n", e.name)
			fmt.Printf("  Peer: %s\n", prof.PeerAddr)
			if prof.TurnHost != "" {
				fmt.Printf("  TURN: %s:%s\n", prof.TurnHost, prof.TurnPort)
			}
			fmt.Printf("  Device ID: %s\n", prof.DeviceID)
			fmt.Printf("  Priority: %d\n", prof.Priority)
			fmt.Printf("  Mode: %s\n", e.d.Mode)
			if e.d.Mode == "socks" {
				fmt.Printf("  SOCKS5 порт: %d\n", e.d.SocksPort)
			}
			fmt.Printf("  PID: %d\n", e.d.PID)

			// Per-profile resource usage
			if usage, err := getProcessUsageByPID(e.d.PID); err == nil {
				fmt.Printf("  Активных воркеров: %d\n", usage.Workers)
			} else {
				fmt.Printf("  [ERROR] не удалось получить статистику: %v\n", err)
			}
			fmt.Println()
		}
	} else {
		fmt.Println("[!] Нет запущенных профилей (PID-файлы не найдены)")
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

	// Show aggregate resource usage
	if usage, err := getProcessUsage(); err == nil {
		fmt.Printf("Использование ресурсов (qwdtt):\n")
		fmt.Printf("  CPU: %.1f%%\n", usage.CPU)
		fmt.Printf("  RAM: %s\n", formatBytes(usage.Memory))
	} else {
		fmt.Printf("Использование ресурсов (qwdtt):\n  [ERROR] %v\n", err)
	}
}

func shareCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt share <name>\n")
		os.Exit(1)
	}

	name := os.Args[2]
	prof, err := loadProfile(name)
	if err != nil {
		log.Fatalf("Ошибка загрузки профиля: %v", err)
	}

	// Get share URL
	var link string
	if prof.LinkFile != "" {
		// Try to read original wdtt:// URL from link file
		linkData, err := os.ReadFile(prof.LinkFile)
		if err == nil {
			link = strings.TrimSpace(string(linkData))
		}
	}

	// Fallback: reconstruct from profile data
	if link == "" {
		// Parse PeerAddr (IP:PORT)
		ip := ""
		dtlsPort := ""
		if idx := strings.LastIndex(prof.PeerAddr, ":"); idx != -1 {
			ip = prof.PeerAddr[:idx]
			dtlsPort = prof.PeerAddr[idx+1:]
		} else {
			ip = prof.PeerAddr
		}

		// Build hashes string
		hashStr := strings.Join(prof.Hashes, ",")

		// Use profile name as the display name
		displayName := name
		if strings.HasPrefix(name, "ro-") {
			displayName = name[3:] // strip "ro-" prefix for display
		}

		// Reconstruct wdtt:// URL with defaults for missing PORT2 (0) and PORT3 (9000)
		link = fmt.Sprintf("wdtt://%s:%s:0:9000:%s:%s#%s", ip, dtlsPort, prof.Password, hashStr, displayName)
	}

	// Output
	fmt.Printf("Профиль: %s\n", name)
	fmt.Printf("Ссылка: %s\n\n", link)
	fmt.Println("QR-код:")
	qrterminal.Generate(link, qrterminal.L, os.Stdout)
	fmt.Println()
}
