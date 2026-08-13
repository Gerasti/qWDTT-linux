package main

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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

// splitFlagsAndArgs separates command-line arguments into flag arguments and
// positional arguments, handling interspersed ordering.
// This allows flags to appear after positional args (e.g., "edit p1 p2 -flag val").
func splitFlagsAndArgs(fs *flag.FlagSet, args []string) (flagArgs []string, positionals []string) {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && args[i] != "-" {
			if strings.Contains(args[i], "=") {
				flagArgs = append(flagArgs, args[i])
				continue
			}
			name := strings.TrimLeft(args[i], "-")
			isBool := false
			if f := fs.Lookup(name); f != nil {
				if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
					isBool = true
				}
			}
			if name == "h" || name == "help" {
				isBool = true
			}
			if isBool {
				flagArgs = append(flagArgs, args[i])
			} else {
				if i+1 < len(args) {
					flagArgs = append(flagArgs, args[i], args[i+1])
					i++
				} else {
					flagArgs = append(flagArgs, args[i])
				}
			}
		} else {
			positionals = append(positionals, args[i])
		}
	}
	return flagArgs, positionals
}

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

	// Check if profile already exists
	_, err = loadProfile(name)
	profileExisted := err == nil

	if err := saveProfile(name, prof); err != nil {
		log.Fatalf("Ошибка сохранения профиля: %v", err)
	}

	if profileExisted {
		fmt.Printf("[OK] Профиль '%s' обновлён\n", name)
	} else {
		fmt.Printf("[OK] Профиль '%s' добавлен\n", name)
	}
	if link.Name != "" && link.Name != "Server" {
		fmt.Printf("  Название: %s\n", link.Name)
	}
	fmt.Printf("  Peer: %s\n", prof.PeerAddr)
	fmt.Printf("  Хешей: %d\n", len(prof.Hashes))
}

func editCmd() {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	peer := fs.String("peer", "", "Адрес сервера (IP:PORT)")
	password := fs.String("password", "", "Пароль")
	hashes := fs.String("hashes", "", "VK-хеши через запятую")
	deviceID := fs.String("device-id", "", "Device ID")
	listen := fs.String("listen", "", "Локальный адрес")
	priority := fs.Int("priority", -1, "Приоритет (чем выше, тем раньше)")
	groups := fs.String("groups", "", "Группы через запятую (пустая строка или none для очистки)")
	group := fs.String("group", "", "Operate on all profiles in this group")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt edit <name1> [name2] ... [флаги] [-group GROUP]\n")
		os.Exit(1)
	}

	anyFlags := false
	groupsChanged := false
	fs.Visit(func(f *flag.Flag) {
		anyFlags = true
		if f.Name == "groups" {
			groupsChanged = true
		}
	})

	if !anyFlags {
		fmt.Println("[!] Не указаны параметры для изменения")
		fmt.Println("Используйте: -peer, -password, -hashes, -device-id, -listen, -priority или -groups")
		os.Exit(1)
	}

	var hadErrors bool
	for _, name := range names {
		if strings.HasPrefix(name, "ro-") {
			fmt.Fprintf(os.Stderr, "[ERROR] Cannot edit read-only profile '%s'. Read-only profiles can only be enabled/disabled.\n", name)
			hadErrors = true
			continue
		}

		prof, err := loadProfile(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		changed := false

		if *peer != "" {
			prof.PeerAddr = *peer
			changed = true
			fmt.Printf("[*] %s: Peer изменён: %s\n", name, *peer)
		}

		if *password != "" {
			prof.Password = *password
			changed = true
			fmt.Printf("[*] %s: Пароль изменён\n", name)
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
			fmt.Printf("[*] %s: Хеши изменены (%d шт.)\n", name, len(prof.Hashes))
		}

		if *deviceID != "" {
			prof.DeviceID = *deviceID
			changed = true
			fmt.Printf("[*] %s: Device ID изменён: %s\n", name, *deviceID)
		}

		if *listen != "" {
			prof.Listen = *listen
			changed = true
			fmt.Printf("[*] %s: Listen изменён: %s\n", name, *listen)
		}

		if *priority != -1 {
			prof.Priority = *priority
			changed = true
			fmt.Printf("[*] %s: Приоритет изменён: %d\n", name, *priority)
		}

		if groupsChanged {
			val := strings.TrimSpace(*groups)
			if val == "" || val == "none" {
				prof.Groups = nil
				changed = true
				fmt.Printf("[*] %s: Группы очищены\n", name)
			} else {
				prof.Groups = nil
				for _, g := range strings.Split(val, ",") {
					g = strings.TrimSpace(g)
					if g != "" && g != "none" {
						prof.Groups = append(prof.Groups, g)
					}
				}
				changed = true
				fmt.Printf("[*] %s: Группы изменены: %v\n", name, prof.Groups)
			}
		}

		if !changed {
			fmt.Printf("[!] %s: параметры не изменены (все значения совпадают с текущими)\n", name)
			continue
		}

		if err := saveProfile(name, *prof); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		fmt.Printf("[OK] Профиль '%s' обновлён\n", name)
	}

	if hadErrors {
		os.Exit(1)
	}
}

func removeCmd() {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	group := fs.String("group", "", "Operate on all profiles in this group")
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt remove <name1> [name2] ... [-group GROUP] [-y]\n")
		os.Exit(1)
	}

	if !*yes {
		fmt.Printf("Удалить %d профилей:\n", len(names))
		for i, name := range names {
			fmt.Printf("  %d. %s\n", i+1, name)
		}
		fmt.Print("Продолжить? [y/N]: ")
		var answer string
		if _, err := fmt.Scanf("%s", &answer); err != nil {
			answer = ""
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("[OK] Отменено")
			return
		}
	}

	var hadErrors bool
	for _, name := range names {
		if strings.HasPrefix(name, "ro-") {
			fmt.Fprintf(os.Stderr, "[ERROR] Cannot remove read-only profile '%s'. Read-only profiles are managed by system configuration.\n", name)
			hadErrors = true
			continue
		}

		if err := os.Remove(profilePath(name)); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		fmt.Printf("[OK] Профиль '%s' удалён\n", name)
	}

	if hadErrors {
		os.Exit(1)
	}
}

func moveCmd() {
	fs := flag.NewFlagSet("move", flag.ExitOnError)
	fs.Parse(os.Args[2:])
	positional := fs.Args()

	if len(positional) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt move <old_name> <new_name>\n")
		os.Exit(1)
	}

	oldName := positional[0]
	newName := positional[1]

	if oldName == newName {
		fmt.Fprintf(os.Stderr, "[ERROR] Old and new names are the same\n")
		os.Exit(1)
	}

	if strings.HasPrefix(oldName, "ro-") || strings.HasPrefix(newName, "ro-") {
		fmt.Fprintf(os.Stderr, "[ERROR] Cannot rename read-only profile. Read-only profiles are managed by system configuration.\n")
		os.Exit(1)
	}

	if _, err := os.Stat(profilePath(oldName)); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Profile %q not found\n", oldName)
		os.Exit(1)
	}

	if _, err := os.Stat(profilePath(newName)); err == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Profile %q already exists\n", newName)
		os.Exit(1)
	}

	if isDaemonRunning(oldName) {
		fmt.Fprintf(os.Stderr, "[ERROR] Profile %q is currently running. Disconnect it first.\n", oldName)
		os.Exit(1)
	}

	if err := renameProfile(oldName, newName); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[OK] Профиль '%s' переименован в '%s'\n", oldName, newName)
}

func listCmd() {
 	type profileInfo struct {
 		name      string
 		peer      string
 		hashes    int
 		status    string
 		priority  int
 		groups    []string
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
				groups:    prof.Groups,
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

	// Parse filter flags
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	en := fs.Bool("en", false, "Show only enabled profiles")
	enabled := fs.Bool("enabled", false, "Show only enabled profiles")
	dis := fs.Bool("dis", false, "Show only disabled profiles")
	disabled := fs.Bool("disabled", false, "Show only disabled profiles")
	ro := fs.Bool("ro", false, "Show only read-only profiles")
	flagArgs, groupFilters := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	showEnabled := *en || *enabled
	showDisabled := *dis || *disabled
	if showEnabled && showDisabled {
		fmt.Fprintln(os.Stderr, "[ERROR] Нельзя одновременно использовать -enabled и -disabled")
		os.Exit(1)
	}

	if len(groupFilters) > 0 {
		filterByGroups := func(src []profileInfo) []profileInfo {
			var filtered []profileInfo
			for _, p := range src {
				for _, g := range p.groups {
					for _, gf := range groupFilters {
						if g == gf {
							filtered = append(filtered, p)
							goto next
						}
					}
				}
			next:
			}
			return filtered
		}
		regularProfiles = filterByGroups(regularProfiles)
		readOnlyProfiles = filterByGroups(readOnlyProfiles)
	}

	// Filter by enabled/disabled status
	if showEnabled || showDisabled {
		filterByStatus := func(src []profileInfo) []profileInfo {
			var filtered []profileInfo
			for _, p := range src {
				if (showEnabled && p.status == "enabled") || (showDisabled && p.status == "disabled") {
					filtered = append(filtered, p)
				}
			}
			return filtered
		}
		regularProfiles = filterByStatus(regularProfiles)
		readOnlyProfiles = filterByStatus(readOnlyProfiles)
	}

	// Filter: -ro shows only read-only profiles
	if *ro {
		regularProfiles = nil
	}

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
		var conditions []string
		if showEnabled {
			conditions = append(conditions, "включённых")
		}
		if showDisabled {
			conditions = append(conditions, "отключённых")
		}
		if *ro {
			conditions = append(conditions, "read-only")
		}
		suffix := ""
		if len(conditions) > 0 {
			suffix = fmt.Sprintf(" (%s)", strings.Join(conditions, ", "))
		}
		if len(groupFilters) > 0 {
			fmt.Printf("Нет профилей в группе/группах '%s'%s\n", strings.Join(groupFilters, ", "), suffix)
		} else {
			fmt.Println("Нет сохранённых профилей" + suffix)
		}
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
					autoswitchMarker = colorYellow + " [autoswitch]" + colorReset
				} else {
					modeStr = " [autoswitch]"
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

			groupsStr := ""
			if len(p.groups) > 0 {
				groupsStr = colorCyan + fmt.Sprintf(" [%s]", strings.Join(p.groups, ", ")) + colorReset
			}

			fmt.Printf(" %s %-*s  %-*s  %d хешей  [%s]  priority: %-3d%s%s%s\n",
					activeMarker,
					maxNameLen, p.name,
					maxPeerLen, p.peer,
					p.hashes,
					coloredStatus,
					p.priority,
					groupsStr,
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
	} else {
		activeProfile := getActiveProfile()
		if activeProfile != "" {
			modeStr := ""
			if d, ok := runningDetails[activeProfile]; ok {
				if d.Mode == "socks" {
					modeStr = fmt.Sprintf(" [socks:%d]", d.SocksPort)
				} else {
					modeStr = " [tun]"
				}
			}
			fmt.Printf("\n[*] Текущий профиль: %s%s%s%s\n", colorYellow, activeProfile, colorReset, modeStr)
		}
	}
}

func showCmd() {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	group := fs.String("group", "", "Operate on all profiles in this group")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt show <name1> [name2] ... [-group GROUP]\n")
		os.Exit(1)
	}

	var hadErrors bool
	for _, name := range names {
		prof, err := loadProfile(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
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

		enabled := isProfileEnabled(name)
		status := "enabled"
		if !enabled {
			status = "disabled"
		}
		fmt.Printf("  Status: %s\n", status)
		fmt.Printf("  Priority: %d\n", prof.Priority)
		if len(prof.Groups) > 0 {
			fmt.Printf("  Groups: %s\n", strings.Join(prof.Groups, ", "))
		}

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

		if name != names[len(names)-1] {
			fmt.Println()
		}
	}

	if hadErrors {
		os.Exit(1)
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
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	group := fs.String("group", "", "Operate on all profiles in this group")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt enable <name1> [name2] ... [-group GROUP]\n")
		os.Exit(1)
	}

	var hadErrors bool
	for _, name := range names {
		_, err := loadProfile(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		if isProfileEnabled(name) {
			fmt.Printf("[*] %s: уже включен\n", name)
			continue
		}

		if err := setProfileEnabled(name, true); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		fmt.Printf("[OK] %s: включен\n", name)
	}

	if hadErrors {
		os.Exit(1)
	}
}

func disableCmd() {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	group := fs.String("group", "", "Operate on all profiles in this group")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt disable <name1> [name2] ... [-group GROUP]\n")
		os.Exit(1)
	}

	var hadErrors bool
	for _, name := range names {
		_, err := loadProfile(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		if !isProfileEnabled(name) {
			fmt.Printf("[*] %s: уже отключен\n", name)
			continue
		}

		if err := setProfileEnabled(name, false); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		fmt.Printf("[OK] %s: отключен\n", name)
	}

	if hadErrors {
		os.Exit(1)
	}
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

// expandProfileMasks expands glob patterns (*, ?, [abc]) against all profile names.
// Names without glob metacharacters pass through unchanged. Matches are sorted.
// Masks that match nothing produce a warning on stderr.
func expandProfileMasks(names []string) []string {
	all := listAllProfileNames()
	var out []string
	seen := make(map[string]bool)
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, n := range names {
		if strings.ContainsAny(n, "*?[") {
			matched := false
			for _, p := range all {
				if ok, _ := filepath.Match(n, p); ok {
					add(p)
					matched = true
				}
			}
			if !matched {
				fmt.Fprintf(os.Stderr, "[!] нет профилей по маске '%s'\n", n)
			}
		} else {
			add(n)
		}
	}
	return out
}

// profilesInGroup returns all profile names (from both regular and ro-profiles dirs)
// whose Groups contain the given group name. Sorted for deterministic output.
func profilesInGroup(group string) []string {
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
			prof, err := loadProfile(name)
			if err != nil {
				continue
			}
			for _, g := range prof.Groups {
				if g == group {
					names = append(names, name)
					break
				}
			}
		}
	}
	readFromDir(filepath.Join(configDir(), "profiles"))
	readFromDir(filepath.Join(configDir(), "ro-profiles"))
	sort.Strings(names)
	return names
}

// collectTargets merges profiles selected via -group with explicit profile names,
// preserving order and removing duplicates.
func collectTargets(group string, explicit []string) []string {
	var targets []string
	seen := make(map[string]bool)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		targets = append(targets, name)
	}
	if group != "" {
		for _, name := range profilesInGroup(group) {
			add(name)
		}
	}
	for _, name := range expandProfileMasks(explicit) {
		add(name)
	}
	return targets
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

// listReadOnlyProfileNames returns profile names from the ro-profiles directory only.
func listReadOnlyProfileNames() []string {
	var names []string

	entries, err := os.ReadDir(filepath.Join(configDir(), "ro-profiles"))
	if err != nil {
		return names
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}

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
	wasExplicitlySpecified := false
	if len(os.Args) >= 3 {
		targetProfile = os.Args[2]
		wasExplicitlySpecified = true
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

	// If still no target, or if there are multiple running profiles and no
	// explicit target was given, show interactive selection
	if targetProfile == "" || (len(running) > 1 && !wasExplicitlySpecified) {
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

	// If autoswitch daemon is running and the target is the profile autoswitch is
	// currently using, tell autoswitch to switch to the next profile instead of
	// killing it directly (autoswitch would just reconnect it).
	if isDaemonRunning("autoswitch") && targetProfile != "" && targetProfile != "autoswitch" {
		currentProfile := getAutoswitchCurrentProfile()
		if currentProfile == targetProfile {
			autoswitchPID, err := readPidForProfile("autoswitch")
			if err == nil && autoswitchPID > 0 {
				fmt.Printf("[*] Отключение текущего профиля '%s' от autoswitch (переключение на следующий)...\n", targetProfile)
				syscall.Kill(autoswitchPID, syscall.SIGUSR1)
				clearAutoswitchCurrentProfile()
				notifyDisconnectedSync(targetProfile)
				fmt.Println("[OK] Отключено")
				return
			}
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

	removeSplitCfg(targetProfile)

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

func logCmd() {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	numLines := fs.Int("n", 0, "Number of lines to show (0 = all)")
	follow := fs.Bool("follow", false, "Follow log output in real-time")
	fs.BoolVar(follow, "f", false, "Follow log output (alias for -follow)")

	var name string
	if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
		name = os.Args[2]
		fs.Parse(os.Args[3:])
	} else {
		fs.Parse(os.Args[2:])
		name = getActiveProfile()
		if name == "" {
			name = "autoswitch"
		}
	}

	logFile := logFilePath(name)

	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось прочитать лог '%s': %v\n", logFile, err)
		os.Exit(1)
	}

	content := string(data)

	if *numLines > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > *numLines {
			lines = lines[len(lines)-*numLines:]
		}
		content = strings.Join(lines, "\n")
	}

	fmt.Print(content)

	if *follow {
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		tailLogFile(logFile)
	}
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

// printBlackList outputs the blacklist configuration for a running profile.
// Reads domains from the raw -bl value and/or resolves domains from the -bl-file JSON.
func printBlackList(d *ProfileDetails) {
	var blDomains []string
	if d.BlackList != "" {
		blDomains = splitDomains(d.BlackList)
	}
	if d.BlackListFile != "" {
		fileDomains, err := loadBypassRoutesFile(d.BlackListFile)
		if err != nil {
			fmt.Printf("  Black list file: [ERROR] %v\n", err)
		} else {
			blDomains = append(blDomains, fileDomains...)
		}
	}
	if len(blDomains) > 0 {
		fmt.Printf("  Black list (%d):\n", len(blDomains))
		for i, domain := range blDomains {
			fmt.Printf("    %-25s", domain)
			if (i+1)%3 == 0 || i == len(blDomains)-1 {
				fmt.Println()
			} else {
				fmt.Print("  ")
			}
		}
	} else {
		fmt.Printf("  Black list: (не настроен)\n")
	}
}

func debugCmd() {
	activeProfile := getActiveProfile()

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
				printBlackList(e.d)
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
			printBlackList(e.d)
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
	fmt.Println("QR-код:")
	qrterminal.Generate(link, qrterminal.L, os.Stdout)
	fmt.Printf("Ссылка: \n%s\n", link)

}

type importProfile struct {
	Name           string `json:"name"`
	Peer           string `json:"peer"`
	VKHashes       string `json:"vkHashes"`
	WorkersPerHash int    `json:"workersPerHash"`
	ListenPort     int    `json:"listenPort"`
	Password       string `json:"password"`
	GroupName      string `json:"groupName"`
}

type dryRunRow struct {
	name   string
	peer   string
	hashes string
	listen string
	groups string
}

func importCmd() {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Показать профили без сохранения")
	flagArgs, args := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	if len(args) < 1 || args[0] == "" {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt import <file.json|file.zip> [-dry-run]\n")
		os.Exit(1)
	}

	path := args[0]

	entries, err := loadImportEntries(path)
	if err != nil {
		log.Fatalf("Ошибка загрузки: %v", err)
	}

	if len(entries) == 0 {
		fmt.Println("[!] В файле нет профилей")
		return
	}

	var imported, skipped int
	var savedNames []string
	var dryRows []dryRunRow

	for _, entry := range entries {
		name := normalizeImportName(entry.Name)
		if name == "" {
			fmt.Fprintf(os.Stderr, "[!] Пропущен профиль с пустым именем\n")
			skipped++
			continue
		}

		finalName := name
		counter := 1
		for {
			_, err := loadProfile(finalName)
			if err != nil {
				break
			}
			finalName = fmt.Sprintf("%s(%d)", name, counter)
			counter++
		}

		if finalName != name {
			fmt.Printf("[*] Конфликт имени: '%s' → '%s'\n", name, finalName)
		}

		prof := ProfileData{
			PeerAddr: entry.Peer,
			Password: entry.Password,
			Hashes:   []string{entry.VKHashes},
		}

		if entry.ListenPort > 0 {
			prof.Listen = fmt.Sprintf("127.0.0.1:%d", entry.ListenPort)
		}

		if entry.GroupName != "" {
			prof.Groups = []string{entry.GroupName}
		}

		if *dryRun {
			dryRows = append(dryRows, dryRunRow{
				name:   finalName,
				peer:   prof.PeerAddr,
				hashes: strconv.Itoa(len(prof.Hashes)),
				listen: prof.Listen,
				groups: fmt.Sprintf("%v", prof.Groups),
			})
			savedNames = append(savedNames, finalName)
			continue
		}

		if err := saveProfile(finalName, prof); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Не удалось сохранить профиль '%s': %v\n", finalName, err)
			skipped++
			continue
		}

		savedNames = append(savedNames, finalName)
		imported++
	}

	if *dryRun {
		printDryRun(dryRows)
		fmt.Printf("\n[dry-run] %d профилей будет импортировано\n", len(savedNames))
	} else {
		fmt.Printf("\n[OK] Импортировано: %d профилей, пропущено: %d\n", imported, skipped)
	}
}

func printDryRun(rows []dryRunRow) {
	if len(rows) == 0 {
		return
	}

	maxName, maxPeer, maxListen := 0, 0, 0
	for _, r := range rows {
		if len(r.name) > maxName {
			maxName = len(r.name)
		}
		if len(r.peer) > maxPeer {
			maxPeer = len(r.peer)
		}
		if len(r.listen) > maxListen {
			maxListen = len(r.listen)
		}
	}

	for _, r := range rows {
		fmt.Printf("[dry-run] %-*s  peer=%-*s  hashes=%s  listen=%-*s  groups=%s\n",
			maxName, r.name, maxPeer, r.peer, r.hashes, maxListen, r.listen, r.groups)
	}
}

func normalizeImportName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func loadImportEntries(path string) ([]importProfile, error) {
	lower := strings.ToLower(path)
	isZipByExt := strings.HasSuffix(lower, ".zip")

	if isZipByExt {
		return loadImportFromZip(path)
	}

	if strings.HasSuffix(lower, ".json") {
		return loadImportFromJSONFile(path)
	}

	// Auto-detect by magic bytes
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия файла '%s': %w", path, err)
	}
	header := make([]byte, 4)
	n, _ := io.ReadFull(f, header)
	f.Close()

	if n >= 2 && header[0] == 'P' && header[1] == 'K' {
		return loadImportFromZip(path)
	}

	return loadImportFromJSONFile(path)
}

func loadImportFromJSONFile(path string) ([]importProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла '%s': %w", path, err)
	}
	return parseImportJSON(data)
}

func parseImportJSON(data []byte) ([]importProfile, error) {
	var entries []importProfile
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %w", err)
	}
	return entries, nil
}

func loadImportFromZip(path string) ([]importProfile, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия ZIP архива '%s': %w", path, err)
	}
	defer reader.Close()

	var allEntries []importProfile
	var jsonFound bool

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		lowerName := strings.ToLower(filepath.Base(file.Name))
		if !strings.HasSuffix(lowerName, ".json") {
			continue
		}
		jsonFound = true

		rc, err := file.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Не удалось открыть '%s' в архиве: %v\n", file.Name, err)
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Не удалось прочитать '%s' из архива: %v\n", file.Name, err)
			continue
		}

		entries, err := parseImportJSON(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Ошибка парсинга '%s' в архиве: %v\n", file.Name, err)
			continue
		}

		allEntries = append(allEntries, entries...)
	}

	if !jsonFound {
		return nil, fmt.Errorf("в архиве не найдено JSON файлов")
	}

	return allEntries, nil
}

// splitDomains разбирает CSV-строку доменов: разделяет, trim, dedup, filter пустых.
func splitDomains(raw string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		d := strings.TrimSpace(part)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// loadBypassRoutesFile reads a JSON file and extracts the bypassRoutes field.
// All other fields in the JSON are ignored.
// bypassRoutes can be either a newline-separated string or an array of strings.
// The path may contain a leading ~ which is expanded to $HOME.
// Returns a deduplicated list of domain/IP/CIDR strings.
func loadBypassRoutesFile(path string) ([]string, error) {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("не удалось определить домашнюю директорию для ~ в пути %q: %w", path, err)
		}
		expanded = filepath.Join(home, path[1:])
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл %q: %w", expanded, err)
	}

	var raw struct {
		BypassRoutes json.RawMessage `json:"bypassRoutes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("не удалось распарсить JSON из %q: %w", expanded, err)
	}
	if len(raw.BypassRoutes) == 0 {
		return nil, fmt.Errorf("поле bypassRoutes не найдено или пустое в файле %q", expanded)
	}

	var domains []string
	if err := json.Unmarshal(raw.BypassRoutes, &domains); err != nil {
		var routesStr string
		if err2 := json.Unmarshal(raw.BypassRoutes, &routesStr); err2 != nil {
			return nil, fmt.Errorf("поле bypassRoutes должно быть строкой или массивом строк в файле %q", expanded)
		}
		for _, d := range strings.Split(routesStr, "\n") {
			if d = strings.TrimSpace(d); d != "" {
				domains = append(domains, d)
			}
		}
	}

	seen := make(map[string]bool)
	var out []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out, nil
}
