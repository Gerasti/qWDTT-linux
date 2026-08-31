package main

import (
	"archive/zip"
	"bufio"
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
	"golang.org/x/net/idna"
	"qwdtt/internal/core"
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
	if len(os.Args) > 3 {
		fs.Parse(os.Args[3:])
	}

	usageMsg := "Usage: qwdtt add <name> <wdtt://... or qwdtt://config?name=...> [-device-id ID]\n"

	var name, rawURL string

	// URL-only form: qwdtt add <wdtt://...> or qwdtt add <qwdtt://config?...&name=...>
	if len(os.Args) >= 3 && (strings.HasPrefix(os.Args[2], "wdtt://") || strings.HasPrefix(os.Args[2], "qwdtt://")) {
		rawURL = os.Args[2]
	} else {
		// Traditional form: qwdtt add <name> <url>
		if len(os.Args) < 4 {
			fmt.Fprint(os.Stderr, usageMsg)
			os.Exit(1)
		}
		name = os.Args[2]
		rawURL = fs.Arg(0)
	}

	if rawURL == "" {
		fmt.Fprint(os.Stderr, usageMsg)
		os.Exit(1)
	}

	link, err := parseLinkWithHint(rawURL)
	if err != nil {
		log.Fatalf("Ошибка парсинга URL: %v", err)
	}

	// In URL-only form, use the name from the link (qwdtt://config?name=... or wdtt://...#Name)
	if name == "" {
		name = link.Name
		if name == "" || name == "Server" {
			fmt.Fprint(os.Stderr, usageMsg)
			os.Exit(1)
		}
	}

	name = transliterateCyrillic(name)

	devID := *deviceID
	if devID == "" {
		devID = getOrCreateDeviceID()
	}

	// Check if profile already exists
	existing, err := loadProfile(name)
	profileExisted := err == nil

	listen := "127.0.0.1:" + defaultListenPort
	if link.Port != "" {
		listen = "127.0.0.1:" + link.Port
	}

	workers := link.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}

	prof := ProfileData{
		PeerAddr: fmt.Sprintf("%s:%s", link.IP, link.DTLSPort),
		Password: link.Password,
		Hashes:   link.Hashes,
		Listen:   listen,
		Workers:  workers,
		DeviceID: devID,
	}

	if profileExisted && len(existing.Groups) > 0 {
		prof.Groups = existing.Groups
	}

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
	if prof.Workers != defaultWorkers {
		fmt.Printf("  Workers: %d\n", prof.Workers)
	}
	if link.Port != "" && link.Port != defaultListenPort {
		fmt.Printf("  Port: %s\n", link.Port)
	}
}

func editCmd() {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	peer := fs.String("peer", "", "Адрес сервера (IP:PORT)")
	password := fs.String("password", "", "Пароль")
	hashes := fs.String("hashes", "", "VK-хеши через запятую")
	deviceID := fs.String("device-id", "", "Device ID")
	listen := fs.String("listen", "", "Локальный адрес")
	priority := fs.Int("priority", -1, "Приоритет (чем выше, тем раньше)")
	workers := fs.Int("workers", -1, "Количество воркеров (должно быть кратно 9)")
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
		fmt.Println("Используйте: -peer, -password, -hashes, -device-id, -listen, -priority, -workers или -groups")
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

		if *workers != -1 {
			if err := validateWorkers(*workers); err != nil {
				fmt.Fprintf(os.Stderr, "[ERROR] %s: workers: %v\n", name, err)
				hadErrors = true
				continue
			}
			prof.Workers = *workers
			changed = true
			fmt.Printf("[*] %s: Workers изменён: %d\n", name, *workers)
		}

		if groupsChanged {
			if prof.Subscription != "" {
				fmt.Fprintf(os.Stderr, "[ERROR] %s: нельзя изменить группы профиля управляемого подпиской '%s'\n", name, prof.Subscription)
				hadErrors = true
				continue
			}

			val := strings.TrimSpace(*groups)
			if val == "" || val == "none" {
				prof.Groups = nil
				changed = true
				fmt.Printf("[*] %s: Группы очищены\n", name)
			} else {
				var newGroups []string
				var blocked []string
				for _, g := range strings.Split(val, ",") {
					g = strings.TrimSpace(g)
					if g == "" || g == "none" {
						continue
					}
					if isSubscriptionName(g) {
						blocked = append(blocked, g)
						continue
					}
					newGroups = append(newGroups, g)
				}
				if len(blocked) > 0 {
					fmt.Fprintf(os.Stderr, "[ERROR] %s: нельзя добавить профиль в групп(у/и) подписки: %s\n", name, strings.Join(blocked, ", "))
					hadErrors = true
					continue
				}
				prof.Groups = newGroups
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

	// Pre-filter: block subscription-managed and read-only profiles before
	// showing the confirmation prompt.
	var filterErrors bool
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "ro-") {
			fmt.Fprintf(os.Stderr, "[ERROR] Cannot remove read-only profile '%s'. Read-only profiles are managed by system configuration.\n", name)
			filterErrors = true
			continue
		}
		if prof, err := loadProfile(name); err == nil && prof.Subscription != "" {
			fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' управляется подпиской '%s'. Используйте: qwdtt sub remove %s\n", name, prof.Subscription, prof.Subscription)
			filterErrors = true
			continue
		}
		filtered = append(filtered, name)
	}
	names = filtered

	if len(names) == 0 {
		if len(os.Args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: qwdtt remove <name1> [name2] ... [-group GROUP] [-y]\n")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "[ERROR] Нет профилей для удаления")
		if filterErrors {
			os.Exit(1)
		}
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

	if prof, err := loadProfile(oldName); err == nil && prof.Subscription != "" {
		fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' управляется подпиской '%s'. Переименование недоступно.\n", oldName, prof.Subscription)
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
		name         string
		peer         string
		hashes       int
		status       string
		priority     int
		groups       []string
		active       bool
		mode         string // "tun" or "socks" (only set if active)
		socksPort    int    // SOCKS5 port (only set if active)
		readOnly     bool
		subscription string // subscription name if managed, "" otherwise
	}

	var regularProfiles []profileInfo
	var readOnlyProfiles []profileInfo
	var subscriptionProfiles []profileInfo
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
				name:         name,
				peer:         prof.PeerAddr,
				hashes:       len(prof.Hashes),
				status:       status,
				priority:     prof.Priority,
				groups:       prof.Groups,
				active:       active,
				mode:         mode,
				socksPort:    socksPort,
				readOnly:     isReadOnly,
				subscription: prof.Subscription,
			}

			if isReadOnly {
				readOnlyProfiles = append(readOnlyProfiles, info)
			} else if prof.Subscription != "" {
				subscriptionProfiles = append(subscriptionProfiles, info)
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
	active := fs.Bool("active", false, "Show only running profiles")
	A := fs.Bool("A", false, "Show only running profiles (alias for -active)")
	sub := fs.Bool("sub", false, "Show only profiles managed by any subscription")
	noIP := fs.Bool("no-ip", false, "Do not display profile server IPs (peer column)")
	flagArgs, groupFilters := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	showEnabled := *en || *enabled
	showDisabled := *dis || *disabled
	showActive := *active || *A
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
		subscriptionProfiles = filterByGroups(subscriptionProfiles)
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
		subscriptionProfiles = filterByStatus(subscriptionProfiles)
	}

	// Filter: -ro shows only read-only profiles
	if *ro {
		regularProfiles = nil
		subscriptionProfiles = nil
	}

	// Filter: -sub shows only profiles managed by any subscription
	if *sub {
		filterBySub := func(src []profileInfo) []profileInfo {
			var filtered []profileInfo
			for _, p := range src {
				if p.subscription != "" {
					filtered = append(filtered, p)
				}
			}
			return filtered
		}
		regularProfiles = nil
		readOnlyProfiles = nil
		subscriptionProfiles = filterBySub(subscriptionProfiles)
	}

	// Filter: -active/-A shows only running profiles
	if showActive {
		filterActive := func(src []profileInfo) []profileInfo {
			var filtered []profileInfo
			for _, p := range src {
				if p.active {
					filtered = append(filtered, p)
				}
			}
			return filtered
		}
		regularProfiles = filterActive(regularProfiles)
		readOnlyProfiles = filterActive(readOnlyProfiles)
		subscriptionProfiles = filterActive(subscriptionProfiles)
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
	sortProfilesByPriority(subscriptionProfiles)

	if len(regularProfiles) == 0 && len(readOnlyProfiles) == 0 && len(subscriptionProfiles) == 0 {
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
		if showActive {
			conditions = append(conditions, "работающих")
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
				} else if p.mode == "raw" {
					modeStr = " [raw]"
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
			paddedMode := fmt.Sprintf("%-15s", modeStr)
			coloredPaddedMode := paddedMode
			if p.active && p.mode == "socks" {
				coloredPaddedMode = colorCyan + paddedMode + colorReset
			}
			if isAutoswitchCurrent && !p.active {
				coloredPaddedMode = colorYellow + paddedMode + colorReset
			}

			// For active autoswitch profile, don't pad mode string to reduce spacing before [autoswitch]
			if isAutoswitchCurrent && p.active {
				if p.active && p.mode == "socks" {
					coloredPaddedMode = colorCyan + modeStr + colorReset
				} else {
					coloredPaddedMode = modeStr
				}
			}

			groupsStr := ""
			if len(p.groups) > 0 {
				groupsStr = colorCyan + fmt.Sprintf(" [%s]", strings.Join(p.groups, ", ")) + colorReset
			}

			// Build the line dynamically to optionally hide the peer (IP) column
			lineFmt := " %s %-*s"
			lineArgs := []interface{}{activeMarker, maxNameLen, p.name}
			if !*noIP {
				lineFmt += "  %-*s"
				lineArgs = append(lineArgs, maxPeerLen, p.peer)
			}
			lineFmt += "  %d хешей  [%s]  priority: %-3d%s%s%s\n"
			lineArgs = append(lineArgs, p.hashes, coloredStatus, p.priority, groupsStr, coloredPaddedMode, autoswitchMarker)
			fmt.Printf(lineFmt, lineArgs...)
		}
	}

	printProfiles(regularProfiles, "Профили")
	printProfiles(subscriptionProfiles, "Подписки")
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
				} else if d.Mode == "raw" {
					modeStr = " [raw]"
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
	sub := fs.Bool("sub", false, "Show all profiles managed by any subscription")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	if *sub && *group == "" && len(names) == 0 {
		names = profilesInAllSubscriptions()
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "[ERROR] нет профилей, управляемых подписками\n")
			os.Exit(1)
		}
	}

	names = collectTargets(*group, names)
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt show <name1> [name2] ... [-group GROUP] [-sub]\n")
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
		workers := prof.Workers
		if workers <= 0 {
			workers = defaultWorkers
		}
		fmt.Printf("  Workers: %d\n", workers)

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
		if prof.Subscription != "" {
			fmt.Printf("  Подписка: %s (группы нельзя изменить)\n", prof.Subscription)
		}

		details := getRunningProfileDetails()
		if d, ok := details[name]; ok {
			fmt.Printf("  Режим: %s\n", d.Mode)
			if d.Mode == "socks" {
				fmt.Printf("  SOCKS5 порт: %d\n", d.SocksPort)
				if d.SocksPublic {
					fmt.Printf("  SOCKS5 прослушивание: 0.0.0.0 (public)\n")
				} else if d.SocksListenAddr != "" {
					fmt.Printf("  SOCKS5 прослушивание: %s\n", d.SocksListenAddr)
				}
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
	ro := fs.Bool("ro", false, "Only operate on read-only profiles")
	sub := fs.Bool("sub", false, "Operate on all profiles managed by any subscription")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	if *ro && len(names) == 0 && *group == "" && !*sub {
		names = listReadOnlyProfileNames()
	}

	if *sub && len(names) == 0 && *group == "" {
		names = profilesInAllSubscriptions()
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "[ERROR] нет профилей, управляемых подписками\n")
			os.Exit(1)
		}
	}

	names = collectTargets(*group, names)

	if *ro && len(names) > 0 {
		for _, name := range names {
			if !strings.HasPrefix(name, "ro-") {
				fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не является read-only\n", name)
				os.Exit(1)
			}
		}
	}

	if *sub && len(names) > 0 {
		for _, name := range names {
			prof, err := loadProfile(name)
			if err != nil || prof.Subscription == "" {
				fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не управляется подпиской\n", name)
				os.Exit(1)
			}
		}
	}

	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt enable <name1> [name2] ... [-group GROUP] [-ro] [-sub]\n")
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
	ro := fs.Bool("ro", false, "Only operate on read-only profiles")
	sub := fs.Bool("sub", false, "Operate on all profiles managed by any subscription")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	if *ro && len(names) == 0 && *group == "" && !*sub {
		names = listReadOnlyProfileNames()
	}

	if *sub && len(names) == 0 && *group == "" {
		names = profilesInAllSubscriptions()
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "[ERROR] нет профилей, управляемых подписками\n")
			os.Exit(1)
		}
	}

	names = collectTargets(*group, names)

	if *ro && len(names) > 0 {
		for _, name := range names {
			if !strings.HasPrefix(name, "ro-") {
				fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не является read-only\n", name)
				os.Exit(1)
			}
		}
	}

	if *sub && len(names) > 0 {
		for _, name := range names {
			prof, err := loadProfile(name)
			if err != nil || prof.Subscription == "" {
				fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не управляется подпиской\n", name)
				os.Exit(1)
			}
		}
	}

	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt disable <name1> [name2] ... [-group GROUP] [-ro] [-sub]\n")
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
		if profiles[i].priority != profiles[j].priority {
			return profiles[i].priority > profiles[j].priority
		}
		return profiles[i].name < profiles[j].name
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
		// URL args (wdtt://, qwdtt://) must pass through without glob matching
		// — qwdtt:// URLs contain '?' which would be mistaken for a glob pattern.
		if strings.HasPrefix(n, "wdtt://") || strings.HasPrefix(n, "qwdtt://") {
			add(n)
			continue
		}
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

// listNonReadOnlyProfileNames returns profile names from the user profiles
// directory only (i.e. excluding read-only / ro-* profiles managed by the system).
func listNonReadOnlyProfileNames() []string {
	var names []string

	entries, err := os.ReadDir(filepath.Join(configDir(), "profiles"))
	if err != nil {
		return names
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasPrefix(name, "ro-") {
			continue
		}
		names = append(names, name)
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
	fs := flag.NewFlagSet("disconnect", flag.ExitOnError)
	all := fs.Bool("all", false, "Отключить все запущенные профили")
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")
	flagArgs, names := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

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

	// --all mode: disconnect every running profile
	if *all {
		if len(names) > 0 {
			fmt.Fprintf(os.Stderr, "[ERROR] --all несовместим с указанием профиля\n")
			os.Exit(1)
		}
		disconnectAllProfiles(running, runningDetails, *yes)
		return
	}

	var targetProfile string
	wasExplicitlySpecified := false
	if len(names) > 0 {
		targetProfile = names[0]
		wasExplicitlySpecified = true
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
					} else if d.Mode == "raw" {
						detailStr = " [raw]"
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

	// Determine the mode of the profile being disconnected
	disconnectMode := "tun"
	if d, ok := runningDetails[targetProfile]; ok {
		disconnectMode = d.Mode
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
				notifyDisconnectedSync(targetProfile, disconnectMode)
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

	// Read saved bypass route CIDRs from state file BEFORE removing it.
	// Used as a fallback to clean up orphaned routes if the daemon did not
	// shut down gracefully (e.g. SIGKILL).
	// In autoswitch mode, also check the current profile's state file.
	var savedSplitRoutes []string
	if autoswitchCurrent := getAutoswitchCurrentProfile(); autoswitchCurrent != "" && autoswitchCurrent != targetProfile {
		_, _, curRoutes, _, _ := readSplitCfgFull(autoswitchCurrent)
		savedSplitRoutes = append(savedSplitRoutes, curRoutes...)
	}
	_, _, targetRoutes, _, _ := readSplitCfgFull(targetProfile)
	savedSplitRoutes = append(savedSplitRoutes, targetRoutes...)

	removeSplitCfg(targetProfile)
	// Also clean up autoswitch current profile state file if it exists
	if autoswitchCurrent := getAutoswitchCurrentProfile(); autoswitchCurrent != "" && autoswitchCurrent != targetProfile {
		removeSplitCfg(autoswitchCurrent)
	}

	// Notify that the profile was disconnected
	notifyDisconnectedSync(targetProfile, disconnectMode)

	// If disconnecting the active profile, clear it.
	// In raw mode, teardownWG() would try to delete wg-qwdtt (which doesn't exist),
	// return nil (table not found), and print a misleading "WireGuard конфиг удален".
	// Raw TUN cleanup already happened via the deferred teardownRawTUN() in core.go
	// when killByPidFile sent SIGINT and the process shut down gracefully.
	if wasActive {
		if disconnectMode != "raw" {
			if err := teardownWG(); err == nil {
				fmt.Println("[OK] WireGuard конфиг удалён")
			}
		} else {
			fmt.Println("[OK] Raw TUN очищен")
		}
		clearActiveProfile()
	}

	// Fallback: remove any orphaned bypass routes that the daemon could not
	// clean up (e.g. after SIGKILL). For TUN mode, teardownWG() already removed
	// tunnel routes and the wg-qwdtt interface; bypass routes via the default
	// gateway remain and are cleaned here. For raw mode, teardownWG() is not
	// called, so bypass routes are cleaned here as well.
	if len(savedSplitRoutes) > 0 {
		if err := core.ClearBypassRoutes(savedSplitRoutes); err != nil {
			fmt.Printf("[WARNING] Не удалось очистить некоторые bypass-маршруты: %v\n", err)
		} else {
			fmt.Printf("[OK] Bypass-маршруты очищены (%d CIDR)\n", len(savedSplitRoutes))
		}
	}

	fmt.Println("[OK] Отключено")
}

// disconnectSingleProfile kills the daemon process for the given profile and
// performs per-profile state cleanup (split-cfg removal, notification).
// It does NOT handle autoswitch SIGUSR1 switching or WireGuard interface
// teardown — those are handled by the caller.
// When skipKill is true, the PID-file/pgrep kill step is skipped (e.g. for
// a profile whose autoswitch daemon has already been terminated).
// Returns the list of saved bypass-route CIDRs for orphaned-route cleanup.
func disconnectSingleProfile(targetProfile string, runningDetails map[string]*ProfileDetails, skipKill bool) []string {
	disconnectMode := "tun"
	if d, ok := runningDetails[targetProfile]; ok {
		disconnectMode = d.Mode
	}

	if !skipKill {
		fmt.Printf("[*] Отключение профиля '%s'...\n", targetProfile)
		if !killByPidFile(targetProfile) {
			fmt.Printf("[*] Pid файл не найден, fallback на pgrep...\n")
			killByPgrep(targetProfile)
		}
	}

	_, _, targetRoutes, _, _ := readSplitCfgFull(targetProfile)
	removeSplitCfg(targetProfile)

	notifyDisconnectedSync(targetProfile, disconnectMode)

	return targetRoutes
}

// disconnectAllProfiles stops every running profile (including the autoswitch
// daemon) and performs global cleanup (WireGuard teardown, active-profile
// clearing, bypass-route removal).
func disconnectAllProfiles(running []string, runningDetails map[string]*ProfileDetails, skipConfirm bool) {
	if len(running) == 0 {
		fmt.Println("[!] Нет активных подключений")
		os.Exit(1)
	}

	// Sort for consistent output (tun first, then socks, then autoswitch)
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

	if !skipConfirm {
		fmt.Printf("[!] Отключить все активные профили: %v\n", running)
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

	// Capture the autoswitch current profile before killing the daemon —
	// that profile's process is owned by autoswitch, so it must be skipped
	// during the per-profile kill step.
	autoswitchRunning := isDaemonRunning("autoswitch")
	autoswitchCurrent := ""
	if autoswitchRunning {
		autoswitchCurrent = getAutoswitchCurrentProfile()
	}

	// Stop autoswitch daemon first so it doesn't restart profiles we're
	// about to kill.
	if autoswitchRunning {
		fmt.Println("[*] Остановка autoswitch daemon...")
		if !killByPidFile("autoswitch") {
			fmt.Printf("[*] Pid файл autoswitch не найден, fallback на pgrep...\n")
			killByPgrep("autoswitch")
		}
		clearAutoswitchCurrentProfile()
	}

	activeProfile := getActiveProfile()
	wasActive := false
	activeWasRaw := false

	// Disconnect each profile individually
	var allSavedRoutes []string
	for _, profile := range running {
		if profile == "autoswitch" {
			continue
		}
		// Skip the kill step for profiles without their own PID file
		// (e.g. autoswitch-managed profiles) or for the explicit
		// autoswitch current — those processes are already terminated.
		_, pidErr := os.Stat(pidFilePath(profile))
		hasOwnPidFile := pidErr == nil
		skipKill := !hasOwnPidFile || profile == autoswitchCurrent
		routes := disconnectSingleProfile(profile, runningDetails, skipKill)
		allSavedRoutes = append(allSavedRoutes, routes...)
		if profile == activeProfile {
			wasActive = true
			if d, ok := runningDetails[profile]; ok && d.Mode == "raw" {
				activeWasRaw = true
			}
		}
	}

	// Tear down WireGuard interface (only the active profile uses wg-qwdtt)
	if wasActive {
		if activeWasRaw {
			fmt.Println("[OK] Raw TUN очищен")
		} else {
			if err := teardownWG(); err == nil {
				fmt.Println("[OK] WireGuard конфиг удалён")
			}
		}
		clearActiveProfile()
	}

	// Fallback: remove any orphaned bypass routes that the daemons could not
	// clean up (e.g. after SIGKILL).
	if len(allSavedRoutes) > 0 {
		if err := core.ClearBypassRoutes(allSavedRoutes); err != nil {
			fmt.Printf("[WARNING] Не удалось очистить некоторые bypass-маршруты: %v\n", err)
		} else {
			fmt.Printf("[OK] Bypass-маршруты очищены (%d CIDR)\n", len(allSavedRoutes))
		}
	}

	fmt.Println("[OK] Все профили отключены")
}

func switchCmd() {
	if !isDaemonRunning("autoswitch") {
		fmt.Fprintln(os.Stderr, "[ERROR] Авто-переключение не запущено. Используйте: qwdtt con -auto-switch")
		os.Exit(1)
	}

	currentProfile := getAutoswitchCurrentProfile()

	pid, err := readPidForProfile("autoswitch")
	if err != nil || pid <= 0 {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось определить PID autoswitch daemon: %v\n", err)
		os.Exit(1)
	}

	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось отправить сигнал autoswitch daemon: %v\n", err)
		os.Exit(1)
	}

	clearAutoswitchCurrentProfile()

	if currentProfile != "" {
		mode := "tun"
		runningDetails := getRunningProfileDetails()
		if d, ok := runningDetails[currentProfile]; ok {
			mode = d.Mode
		}
		notifyDisconnectedSync(currentProfile, mode)
		fmt.Printf("[OK] Переключение с профиля '%s' на следующий...\n", currentProfile)
	} else {
		fmt.Println("[OK] Переключение на следующий профиль...")
	}
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

// displayDomain returns the Unicode form of an IDN domain (xn--...) for
// human-readable output. Non-IDN domains are returned unchanged; on error
// the original domain is returned so listings never break.
// This is a pure display helper — files keep their punycode form, since
// DNS resolvers and tunnel backends require the ascii (xn--) variant.
func displayDomain(d string) string {
	if !strings.Contains(d, "xn--") {
		return d
	}
	if u, err := idna.ToUnicode(d); err == nil && u != "" {
		return u
	}
	return d
}

// formatDomainsInColumns lays domains out into a fixed number of columns
// (column-major, like `ls`), each column padded to the longest domain. indent
// prefixes every line; sep separates columns. Trailing whitespace is trimmed.
func formatDomainsInColumns(domains []string, indent, sep string, cols int) string {
	if len(domains) == 0 {
		return ""
	}
	if cols < 1 {
		cols = 1
	}
	maxLen := 0
	for _, d := range domains {
		if len(d) > maxLen {
			maxLen = len(d)
		}
	}
	rows := (len(domains) + cols - 1) / cols
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		var line strings.Builder
		line.WriteString(indent)
		for c := 0; c < cols; c++ {
			idx := r + c*rows
			if idx >= len(domains) {
				break
			}
			line.WriteString(fmt.Sprintf("%-*s", maxLen, domains[idx]))
			if c < cols-1 {
				line.WriteString(sep)
			}
		}
		sb.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	return sb.String()
}

// blDomain carries a per-domain status marker and the domain text. Prefix is
// "pending-add" ("[!]"), "pending-remove" ("[-]") or "" for applied; it only
// tags which domains are unapplied. The actual rendering is done as a per-row
// marker in a separate left column by formatTaggedDomains (variant B2), so the
// marker never participates in domain column width calculation and never
// misaligns the domain grid.
type blDomain struct {
	Prefix string
	Domain string
}

// blMarkerColW is the fixed width of the per-row status column rendered to the
// left of the domain grid: the marker text "[!]"/"[-]" is 3 runes plus one
// separator space. Domains are aligned independently of the marker.
const blMarkerColW = 4

// formatTaggedDomains renders the domain grid with a per-row status marker in
// a fixed-width column to the left (variant B2). The marker reflects the whole
// row: "[!]" if any cell in the row is a pending add, else "[-]" if any cell is
// a pending remove, else blank. Because the marker lives in its own column and
// the domain width is measured without it, domain columns stay aligned
// regardless of which rows carry a marker.
func formatTaggedDomains(tags []blDomain, indent, sep string, cols int) string {
	if len(tags) == 0 {
		return ""
	}
	if cols < 1 {
		cols = 1
	}
	maxLen := 0
	for _, t := range tags {
		if len(t.Domain) > maxLen {
			maxLen = len(t.Domain)
		}
	}
	rows := (len(tags) + cols - 1) / cols
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		var line strings.Builder
		line.WriteString(indent)
		// Row-level marker (B2): a pending add in any cell wins over a pending remove.
		rowMarker := ""
		for c := 0; c < cols; c++ {
			if idx := r + c*rows; idx < len(tags) && tags[idx].Prefix == "[!]" {
				rowMarker = "[!]"
				break
			}
		}
		if rowMarker == "" {
			for c := 0; c < cols; c++ {
				if idx := r + c*rows; idx < len(tags) && tags[idx].Prefix == "[-]" {
					rowMarker = "[-]"
					break
				}
			}
		}
		line.WriteString(fmt.Sprintf("%-*s", blMarkerColW, rowMarker))
		for c := 0; c < cols; c++ {
			idx := r + c*rows
			if idx >= len(tags) {
				break
			}
			line.WriteString(fmt.Sprintf("%-*s", maxLen, tags[idx].Domain))
			if c < cols-1 {
				line.WriteString(sep)
			}
		}
		sb.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	return sb.String()
}

// normalizeBlPath normalizes a bl-file path for comparison: expands ~/$VAR,
// resolves to an absolute path, and cleans it. Empty string stays empty.
func normalizeBlPath(p string) string {
	if p == "" {
		return ""
	}
	expanded, err := expandPath(p)
	if err != nil || expanded == "" {
		expanded = p
	}
	if abs, err := filepath.Abs(expanded); err == nil {
		expanded = abs
	}
	return filepath.Clean(expanded)
}

// isBlFilePending reports whether the bl-file on disk has been modified since
// the daemon last applied it (recorded as appliedMtime in the state file).
// It is true only when the file exists and its mtime is strictly greater than
// the applied mtime, i.e. `qwdtt bl add`/`bl rm` (without -r/--reload) or an
// external edit happened after the last connect / BL_RELOAD.
func isBlFilePending(blFile string, appliedMtime int64) bool {
	if blFile == "" || appliedMtime <= 0 {
		return false
	}
	path := normalizeBlPath(blFile)
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.ModTime().UnixNano() > appliedMtime
}

// isBlPendingForProfile reports whether the profile still running has a bl-file
// with unapplied changes. It prefers the recorded black_list_mtime; for state
// files written by older binaries (mtime == 0) it falls back to comparing the
// bl-file's mtime against the state file's own mtime.
func isBlPendingForProfile(profile string, d *ProfileDetails) bool {
	if d == nil || d.BlackListFile == "" {
		return false
	}
	if d.BlackListMtime > 0 {
		return isBlFilePending(d.BlackListFile, d.BlackListMtime)
	}
	return isBlFilePending(d.BlackListFile, stateFileMtime(profile))
}

// profileBlPending reports whether the running profile described by d has a
// bl-file with unapplied changes. Returns the pending flag and the profile name.
func profileBlPending(profile string, d *ProfileDetails) (bool, string) {
	if d == nil {
		return false, ""
	}
	prof := profile
	if prof == "" {
		prof = "autoswitch"
	}
	return isBlPendingForProfile(prof, d), prof
}

// domainKey returns a normalized, case-insensitive key for comparing domains:
// punycode is decoded, Cyrillic is transliterated, and the result is lowercased.
// So "ВТБ.рф", "втб.рф" and "xn--e1-...6dj" all collapse to the same key.
func domainKey(d string) string {
	return strings.ToLower(transliterateCyrillic(displayDomain(d)))
}

// blProfileForFile finds the running profile whose applied bl-file matches the
// given file path. Returns the profile name, its details, and ok=false if none.
// The autoswitch pseudo-profile is skipped; its current sub-profile is already
// enumerated as a real entry in getRunningProfileDetails.
func blProfileForFile(file string) (prof string, d *ProfileDetails, ok bool) {
	if file == "" {
		return "", nil, false
	}
	target := normalizeBlPath(file)
	if target == "" {
		return "", nil, false
	}
	for name, pd := range getRunningProfileDetails() {
		if name == "autoswitch" {
			continue
		}
		if normalizeBlPath(pd.BlackListFile) != target {
			continue
		}
		return name, pd, true
	}
	return "", nil, false
}

// printBlackList outputs the blacklist configuration for a running profile.
// Reads domains from the raw -bl value and/or resolves domains from the -bl-file JSON.
// When the bl-file on disk has been edited (bl add/bl rm) since the daemon last
// applied it, a pending-changes marker is shown and the profile is reported
// through profileBlPending.
func printBlackList(d *ProfileDetails, profile string) {
	var blDomains []string
	if d.BlackList != "" {
		blDomains = splitDomains(d.BlackList)
	}
	var fileDomains []string
	if d.BlackListFile != "" {
		loaded, err := loadBypassRoutesFile(d.BlackListFile)
		if err != nil {
			fmt.Printf("  Black list file: [ERROR] %v\n", err)
		} else {
			fileDomains = loaded
			blDomains = append(blDomains, fileDomains...)
		}
	}

	pending, prof := profileBlPending(profile, d)
	appliedSet := map[string]bool{}
	for _, ad := range d.BlackListDomains {
		appliedSet[domainKey(ad)] = true
	}

	if len(blDomains) > 0 {
		fmt.Printf("  Black list (%d):\n", len(blDomains))
		if pending && len(appliedSet) > 0 {
			// Pending changes are shown as a per-row marker in a separate left
			// column (variant B2): "[!]" if the row contains a pending add, else
			// "[-]" for a pending remove, else blank. Applied domains are listed
			// plainly; only unapplied domains carry the prefix that drives the row.
			tags := make([]blDomain, len(blDomains))
			for i, dom := range blDomains {
				tags[i] = blDomain{Domain: displayDomain(dom)}
				if _, applied := appliedSet[domainKey(dom)]; !applied {
					tags[i].Prefix = "[!]"
				}
			}
			fmt.Print(formatTaggedDomains(tags, "    ", "  ", 3))
		} else {
			displayed := make([]string, len(blDomains))
			for i, dom := range blDomains {
				displayed[i] = displayDomain(dom)
			}
			fmt.Print(formatDomainsInColumns(displayed, "    ", "  ", 3))
		}
	} else {
		fmt.Printf("  Black list: (не настроен)\n")
	}
	if pending {
		fmt.Printf("  [!] %s: изменения в bl-file не применены (bl add/bl rm без -r). "+
			"Примените: qwdtt bl load %s  или  qwdtt connect %s -r\n",
			prof, d.BlackListFile, prof)
	}
}

func debugCmd() {
	fmt.Printf("=== DEBUG INFO ===\n\n")

	// Show autoswitch status if active. Detect via the live autoswitch PID file
	// rather than active_profile, which is unreliable in auto-switch/socks mode
	// (it can be stale or cleared on profile rotation).
	if isDaemonRunning("autoswitch") {
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
		currentAutoswitchProfile := getAutoswitchCurrentProfile()
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
					if e.d.SocksPublic {
						fmt.Printf("  SOCKS5 прослушивание: 0.0.0.0 (public)\n")
					} else if e.d.SocksListenAddr != "" {
						fmt.Printf("  SOCKS5 прослушивание: %s\n", e.d.SocksListenAddr)
					}
				}
				fmt.Printf("  PID: %d\n", e.d.PID)

				// Show current autoswitch profile
				if currentAutoswitchProfile != "" {
					fmt.Printf("  Текущий профиль: %s\n", currentAutoswitchProfile)
				} else {
					fmt.Printf("  Текущий профиль: (нет активного подключения)\n")
				}

				// Per-profile resource usage
				if usage, err := getProcessUsageByPID(e.d.PID); err == nil {
					fmt.Printf("  Активных воркеров: %d\n", usage.Workers)
				} else {
					fmt.Printf("  [ERROR] не удалось получить статистику: %v\n", err)
				}
				// not on the autoswitch pseudo-profile, so print the current
				// profile's blacklist here (reflects hot-reloaded bl-file).
				if curD, ok := details[currentAutoswitchProfile]; ok {
					printBlackList(curD, currentAutoswitchProfile)
				} else {
					printBlackList(e.d, currentAutoswitchProfile)
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
				if e.d.SocksPublic {
					fmt.Printf("  SOCKS5 прослушивание: 0.0.0.0 (public)\n")
				} else if e.d.SocksListenAddr != "" {
					fmt.Printf("  SOCKS5 прослушивание: %s\n", e.d.SocksListenAddr)
				}
			}
			fmt.Printf("  PID: %d\n", e.d.PID)

			// Per-profile resource usage
			if usage, err := getProcessUsageByPID(e.d.PID); err == nil {
				fmt.Printf("  Активных воркеров: %d\n", usage.Workers)
			} else {
				fmt.Printf("  [ERROR] не удалось получить статистику: %v\n", err)
			}
			if isDaemonRunning("autoswitch") && e.name == currentAutoswitchProfile {
				fmt.Printf("  Black list: (управляется autoswitch — см. выше)\n")
			} else {
				printBlackList(e.d, e.name)
			}
			fmt.Println()
		}
	} else {
		fmt.Println("[!] Нет запущенных профилей (PID-файлы не найдены)")
		return
	}

	iface := wgIface
	if hasRawModeProfile(details) {
		iface = core.RawIfaceName
	}

	if stats, err := getWGStats(iface); err == nil {
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

func hasRawModeProfile(details map[string]*ProfileDetails) bool {
	for _, d := range details {
		if d.Mode == "raw" {
			return true
		}
	}
	return false
}

func shareCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt share <name> [-qwdtt|-q] [-group GROUP]\n")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("share", flag.ExitOnError)
	qwdttFlag := fs.Bool("qwdtt", false, "Generate qwdtt://config? URL format instead of wdtt://")
	fs.BoolVar(qwdttFlag, "q", false, "Alias for -qwdtt")
	group := fs.String("group", "", "Operate on all profiles in this group")
	flagArgs, args := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	var names []string
	if *group != "" {
		names = collectTargets(*group, nil)
	} else {
		names = args
	}

	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt share <name> [-qwdtt|-q] [-group GROUP]\n")
		os.Exit(1)
	}

	for _, name := range names {
		prof, err := loadProfile(name)
		if err != nil {
			log.Fatalf("Ошибка загрузки профиля '%s': %v", name, err)
		}

		// Parse PeerAddr (IP:PORT)
		ip := ""
		dtlsPort := ""
		if idx := strings.LastIndex(prof.PeerAddr, ":"); idx != -1 {
			ip = prof.PeerAddr[:idx]
			dtlsPort = prof.PeerAddr[idx+1:]
		} else {
			ip = prof.PeerAddr
		}

		displayName := name
		if strings.HasPrefix(name, "ro-") {
			displayName = name[3:] // strip "ro-" prefix for display
		}

		var output string

		if *qwdttFlag {
			workers := prof.Workers
			if workers <= 0 {
				workers = defaultWorkers
			}
			port := defaultListenPort
			if prof.Listen != "" {
				if idx := strings.LastIndex(prof.Listen, ":"); idx != -1 {
					port = prof.Listen[idx+1:]
				} else {
					port = prof.Listen
				}
			}
			link := &WdttLink{
				IP:       ip,
				DTLSPort: dtlsPort,
				Password: prof.Password,
				Hashes:   prof.Hashes,
				Name:     displayName,
				Workers:  workers,
				Port:     port,
			}
			output = buildQwdttConfigURL(link)
		} else {
			// Try to read original URL from link file
			var link string
			if prof.LinkFile != "" {
				linkData, err := os.ReadFile(prof.LinkFile)
				if err == nil {
					link = strings.TrimSpace(string(linkData))
				}
			}

			// Fallback: reconstruct wdtt:// URL
			if link == "" {
				hashStr := strings.Join(prof.Hashes, ",")
				link = fmt.Sprintf("wdtt://%s:%s:0:9000:%s:%s#%s", ip, dtlsPort, prof.Password, hashStr, displayName)
			}
			output = link
		}

		if len(names) > 1 {
			fmt.Printf("=== %s ===\n", displayName)
		}

		// Output
		fmt.Println("QR-код:")
		qrterminal.Generate(output, qrterminal.L, os.Stdout)
		fmt.Printf("Ссылка: \n%s\n", output)

		if len(names) > 1 {
			fmt.Println()
		}
	}
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
	name = strings.TrimSpace(name)
	name = transliterateCyrillic(name)
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('_')
		} else if r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

func loadImportEntries(path string) ([]importProfile, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return nil, err
	}
	path = expanded

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

// splitDomains разбирает строку доменов, разделённых пробельными символами
// (пробел, таб, перевод строки): trim, dedup, filter пустых. Запятая в
// отдельном домене — ошибка, т.к. доменные имена не могут содержать запятых.
func splitDomains(raw string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Fields(raw) {
		if strings.Contains(part, ",") {
			fmt.Fprintf(os.Stderr, "[ERROR] домен не может содержать запятую: %q\n", part)
			os.Exit(1)
		}
		if seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

// readBypassFile reads a bypass routes file, expanding '~'/'$VAR' in path.
// Accepts a plain JSON file or a ZIP archive containing a JSON file with a
// "bypassRoutes" field. Returns the raw JSON bytes and the resolved location
// (file path, or "archive/entry.json" for zipped entries).
func readBypassFile(path string) (data []byte, expanded string, err error) {
	expanded, err = expandPath(path)
	if err != nil {
		return nil, "", err
	}
	lower := strings.ToLower(expanded)
	if strings.HasSuffix(lower, ".zip") {
		return loadBypassFromZip(expanded)
	}
	data, err = os.ReadFile(expanded)
	if err != nil {
		return nil, expanded, fmt.Errorf("не удалось прочитать файл %q: %w", expanded, err)
	}
	// ZIP magic bytes but a non-.zip extension — still try it as a zip.
	if len(data) >= 2 && data[0] == 'P' && data[1] == 'K' {
		return loadBypassFromZip(expanded)
	}
	return data, expanded, nil
}

// loadBypassFromZip opens a ZIP archive and returns the raw JSON of the first
// entry whose parsed object contains a "bypassRoutes" field.
func loadBypassFromZip(path string) ([]byte, string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, path, fmt.Errorf("ошибка открытия ZIP %q: %w", path, err)
	}
	defer reader.Close()
	for _, f := range reader.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(filepath.Base(f.Name)), ".json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, rerr := io.ReadAll(rc)
		rc.Close()
		if rerr != nil {
			continue
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(data, &probe) == nil {
			if _, ok := probe["bypassRoutes"]; ok {
				return data, path + "/" + f.Name, nil
			}
		}
	}
	return nil, path, fmt.Errorf("в ZIP %q не найдено JSON с полем bypassRoutes; "+
		"если это export профилей — используйте: qwdtt import %q", path, path)
}

// loadBypassRoutesFile reads a JSON file (or a ZIP containing such a JSON) and
// extracts the bypassRoutes field. All other fields in the JSON are ignored.
// bypassRoutes can be either a newline-separated string or an array of strings.
// Returns a deduplicated list of domain/IP/CIDR strings.
func loadBypassRoutesFile(path string) ([]string, error) {
	data, expanded, err := readBypassFile(path)
	if err != nil {
		return nil, err
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

// expandPath expands a leading '~' to $HOME and resolves $ENV variables in the
// given path. Returns an error if '~' is used but $HOME cannot be determined.
func expandPath(path string) (string, error) {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("не удалось определить домашнюю директорию для ~ в пути %q: %w", path, err)
		}
		expanded = filepath.Join(home, path[1:])
	}
	return expanded, nil
}

// parseBypassRawRoutes extracts domains from the raw JSON value of the
// "bypassRoutes" field. Supports both a JSON array of strings and a JSON
// string of newline-separated values. Returns the domains and a flag
// indicating whether the source was an array.
func parseBypassRawRoutes(raw json.RawMessage) (domains []string, wasArray bool) {
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for _, d := range strings.Split(s, "\n") {
			if d = strings.TrimSpace(d); d != "" {
				domains = append(domains, d)
			}
		}
		return domains, false
	}
	return nil, false
}

// loadBypassForEdit reads a bypass routes JSON file, returning the full raw
// document (to preserve other fields), the list of domains parsed from the
// "bypassRoutes" field, and whether the field was a JSON array.
func loadBypassForEdit(path string) (raw map[string]json.RawMessage, domains []string, isArray bool, err error) {
	data, expanded, err := readBypassFile(path)
	if err != nil {
		return nil, nil, false, err
	}
	var r map[string]json.RawMessage
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil, false, fmt.Errorf("не удалось распарсить JSON из %q: %w", expanded, err)
	}
	if br, ok := r["bypassRoutes"]; ok && len(br) > 0 {
		domains, isArray = parseBypassRawRoutes(br)
	}
	return r, domains, isArray, nil
}

// saveBypassFile writes the bypass routes JSON file, preserving any existing
// top-level fields (format, formatVersion, appVersion, etc.) and keeping the
// bypassRoutes field in the same JSON type it had (string vs array). When
// createNew is true and the document is empty, a default qwdtt-bypass header
// is seeded.
func saveBypassFile(path string, raw map[string]json.RawMessage, domains []string, isArray bool, createNew bool) error {
	if createNew && len(raw) == 0 {
		raw["format"] = json.RawMessage(`"qwdtt-bypass"`)
		raw["formatVersion"] = json.RawMessage(`1`)
		raw["appVersion"] = json.RawMessage(`"` + version + `"`)
	}

	if isArray {
		b, err := json.Marshal(domains)
		if err != nil {
			return err
		}
		raw["bypassRoutes"] = b
	} else {
		b, err := json.Marshal(strings.Join(domains, "\n"))
		if err != nil {
			return err
		}
		raw["bypassRoutes"] = b
	}

	expanded, err := expandPath(path)
	if err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(expanded), ".zip") {
		return fmt.Errorf("нельзя сохранять изменения в ZIP %q; распакуйте JSON с bypassRoutes в файл и редактируйте его", path)
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(expanded, data, 0o644)
}

func blCmd() {
	printBlUsage := func() {
		fmt.Fprintln(os.Stderr, "Usage: qwdtt bl <add|list|remove|find|init|load|unload> [options] [paths/domains...]")
		fmt.Fprintln(os.Stderr, "  list (ls), add, remove (rm), find (fd), init, load <path>, unload [-p PROFILE]")
		fmt.Fprintln(os.Stderr, "  -f, --file PATH   Path to bypass routes JSON file (required, except for init/load)")
		fmt.Fprintln(os.Stderr, "  -p, --profile PROFILE  Target a specific running profile's bl-file")
		fmt.Fprintln(os.Stderr, "  -r, --reload          Hot-reload bl-file to the running daemon after changes (tun/raw/socks)")
	}
	if len(os.Args) < 3 {
		printBlUsage()
		os.Exit(1)
	}
	fs := flag.NewFlagSet("bl", flag.ExitOnError)
	var file string
	fs.StringVar(&file, "file", "", "Path to bypass routes JSON file (required)")
	fs.StringVar(&file, "f", "", "Alias for -file")
	yes := fs.Bool("y", false, "Skip confirmation prompt (remove only)")
	profile := fs.String("profile", "", "Target a specific running profile's bl-file (works for socks)")
	fs.StringVar(profile, "p", "", "Alias for -profile")
	reload := fs.Bool("reload", false, "Hot-reload bl-file to the running daemon after changes (tun/raw/socks)")
	fs.BoolVar(reload, "r", false, "Alias for --reload")
	flagArgs, args := splitFlagsAndArgs(fs, os.Args[2:])
	fs.Parse(flagArgs)

	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	if sub == "init" {
		blInitCmd(args)
		return
	}

	if sub == "load" {
		// Случай 1: путь не указан ни args, ни -f/--file → ошибка (независимо от -p)
		if (len(args) == 0 || args[0] == "") && file == "" {
			fmt.Fprintln(os.Stderr, "[ERROR] Путь к bl-file не указан.")
			fmt.Fprintln(os.Stderr, "Используйте: qwdtt bl load [-p PROFILE] <path>")
			os.Exit(1)
		}
		rawPath := file
		if len(args) > 0 && args[0] != "" {
			rawPath = args[0]
		}

		// Случай 2: путь есть, но нет -p → проверяем наличие запущенного TUN/RAW
		if *profile == "" {
			if _, _, _, rerr := resolveBlSocket(""); rerr != nil {
				fmt.Fprintln(os.Stderr, "[ERROR] Нет запущенного профиля TUN/RAW для hot-reload.")
				fmt.Fprintln(os.Stderr, "Укажите -p/--profile PROFILE или запустите профиль в режиме TUN/RAW.")
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[*] Hot-reload для текущего профиля TUN/RAW\n")
		}
		blLoad([]string{rawPath}, *profile)
		return
	}

	if sub == "unload" {
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "Usage: qwdtt bl unload [-p|--profile PROFILE]")
			os.Exit(1)
		}
		if *profile == "" && getCurrentBlFile() == "" {
			fmt.Fprintln(os.Stderr, "[ERROR] Нет запущенного профиля TUN/RAW для unload.")
			fmt.Fprintln(os.Stderr, "Укажите -p/--profile PROFILE или запустите профиль в режиме TUN/RAW.")
			os.Exit(1)
		}
		if *profile != "" {
			fmt.Fprintf(os.Stderr, "[*] Hot-reload для профиля %q\n", *profile)
		}
		blUnload(*profile)
		return
	}

	if *profile != "" {
		if file != "" {
			fmt.Fprintln(os.Stderr, "[ERROR] нельзя использовать -p/--profile и -f/--file одновременно")
			os.Exit(1)
		}
		file = getBlFileForProfile(*profile)
		if file == "" {
			fmt.Fprintf(os.Stderr, "[ERROR] bl-file не найден для профиля %q: профиль не запущен или не использует -bl-file\n", *profile)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[*] Используется bl-file профиля %q: %s\n", *profile, file)
	} else if file == "" {
		// Auto-detect: use the bl-file of the current running profile.
		file = getCurrentBlFile()
		if file != "" {
			fmt.Fprintf(os.Stderr, "[*] Используется bl-file текущего профиля TUN/RAW: %s\n", file)
		}
	}

	if file == "" {
		printBlUsage()
		os.Exit(1)
	}

	switch sub {
	case "list", "ls":
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "[ERROR] list не принимает позиционные аргументы: %v\n", args)
			os.Exit(1)
		}
		blList(file)
	case "add":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: qwdtt bl add <domain1> [domain2...] -file/-f PATH [-r]")
			os.Exit(1)
		}
		blAdd(args, file, *reload, *profile)
	case "remove", "rm":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: qwdtt bl remove <domain1> [domain2...] -file/-f PATH [-y] [-r]")
			os.Exit(1)
		}
		blRemove(args, file, *yes, *reload, *profile)
	case "find", "fd":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: qwdtt bl find <domain1> [domain2...] -file/-f PATH")
			os.Exit(1)
		}
		blFind(args, file)
	default:
		fmt.Fprintf(os.Stderr, "[ERROR] неизвестная подкоманда '%s'. Доступные: add, list, remove, find, init, load\n", sub)
		os.Exit(1)
	}
}

func blInitCmd(args []string) {
	path := "qwdtt_bl.json"
	if len(args) > 0 {
		path = args[0]
	}

	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] файл %q уже существует. Используйте 'qwdtt bl -f %s add <domains...>' для добавления доменов.\n", path, path)
		os.Exit(1)
	}

	raw := map[string]json.RawMessage{}
	if err := saveBypassFile(path, raw, nil, false, true); err != nil {
		log.Fatalf("Ошибка создания %q: %v", path, err)
	}

	fmt.Printf("[OK] Создан bl-file: %s (0 доменов)\n", path)
}

// resolveBlSocket determines the daemon's control socket path for the target
// profile. If explicitProfile is non-empty it is used directly; otherwise the
// active profile (or the autoswitch sub-profile) is used. The returned mode is
// always one of tun/raw/socks. An error is returned when no suitable running
// daemon can be found.
func resolveBlSocket(explicitProfile string) (sock string, targetProfile, mode string, err error) {
	autoswitchRunning := isDaemonRunning("autoswitch")
	autoswitchCurrent := getAutoswitchCurrentProfile()
	details := getRunningProfileDetails()

	var socketProfile string
	if explicitProfile != "" {
		targetProfile = explicitProfile
		// The autoswitch daemon owns the only control socket that can
		// hot-reload its current sub-profile. Route reloads for that
		// profile to the autoswitch socket; any mode (incl. socks) is allowed
		// because the user explicitly named the target.
		if autoswitchRunning && autoswitchCurrent == targetProfile {
			socketProfile = "autoswitch"
		} else {
			socketProfile = targetProfile
		}
	} else {
		// bl без аргументов работает ТОЛЬКИ с tun/raw (kernel interface):
		// несколько socks-профилей могут работать одновременно, так что
		// active_profile не может их однозначно идентифицировать.
		// Приоритет выбора tun/raw-цели: autoswitch current (если tun/raw) ->
		// active_profile (если tun/raw) -> любой запущенный tun/raw профиль.
		if autoswitchRunning && autoswitchCurrent != "" {
			if d, ok := details[autoswitchCurrent]; ok && (d.Mode == "tun" || d.Mode == "raw") {
				socketProfile, targetProfile = "autoswitch", autoswitchCurrent
			}
		}
		if socketProfile == "" {
			if active := getActiveProfile(); active != "" && active != "autoswitch" {
				if d, ok := details[active]; ok && (d.Mode == "tun" || d.Mode == "raw") {
					socketProfile, targetProfile = active, active
				}
			}
		}
		if socketProfile == "" {
			for name, d := range details {
				if name == "autoswitch" {
					continue
				}
				if d.Mode == "tun" || d.Mode == "raw" {
					socketProfile, targetProfile = name, name
					break
				}
			}
		}
		if socketProfile == "" {
			return "", "", "", fmt.Errorf("нет активного подключения TUN/RAW. Укажите профиль отдельно: -p <profile>")
		}
	}

	d, ok := details[targetProfile]
	if !ok {
		return "", "", "", fmt.Errorf("не удалось определить параметры запущенного профиля %q", targetProfile)
	}
	mode = d.Mode
	if mode != "tun" && mode != "raw" && mode != "socks" {
		return "", "", "", fmt.Errorf("bl reload поддерживается для tun/raw/socks, текущий режим: %q", mode)
	}

	sock = socketPath(socketProfile)
	if _, statErr := os.Stat(sock); statErr != nil {
		return "", "", "", fmt.Errorf("сокет демена не найден: %s (демен не запущен?)", sock)
	}
	return sock, targetProfile, mode, nil
}

// sendBlReloadCommand resolves the daemon's control socket for the target
// profile and sends a BL_RELOAD command so the bypass routes are re-applied.
// When fileAbspath is empty, the daemon re-applies only the raw -bl domains
// (effectively unloading the bl-file). When non-empty, the bl-file at that path
// is reloaded. The explicitProfile (from -p/--profile) locates the socket; when
// empty the active TUN/RAW profile is used.
func sendBlReloadCommand(explicitProfile, fileAbspath string) {
	if explicitProfile == "" && !isKernelInterfaceActive() && !rawIfaceActive() {
		return
	}

	sock, targetProfile, mode, err := resolveBlSocket(explicitProfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}

	if fileAbspath == "" {
		fmt.Printf("[*] Сброс bl-file для профиля %q (режим %s)...\n", targetProfile, mode)
	} else {
		fmt.Printf("[*] Перезагрузка bl-file %q для профиля %q (режим %s)...\n", fileAbspath, targetProfile, mode)
	}

	reply, err := sendBlReload(sock, fileAbspath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}

	if strings.HasPrefix(reply, "RELOAD_OK|") {
		fmt.Printf("[OK] %s\n", strings.TrimPrefix(reply, "RELOAD_OK|"))
	} else if strings.HasPrefix(reply, "RELOAD_ERR|") {
		fmt.Fprintf(os.Stderr, "[ERROR] %s\n", strings.TrimPrefix(reply, "RELOAD_ERR|"))
		os.Exit(1)
	} else {
		fmt.Printf("[*] Ответ демена: %s\n", reply)
	}
}

// reloadBlFile expands/resolves the given file path and sends a BL_RELOAD
// command to the running daemon so that the bypass routes file is re-read
// without reconnecting. The explicitProfile (from -p/--profile) is used to
// locate the daemon's control socket; when empty the active profile is used.
func reloadBlFile(file string, explicitProfile string) {
	expanded, err := expandPath(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
	abspath, err := filepath.Abs(expanded)
	if err != nil {
		abspath = expanded
	}
	sendBlReloadCommand(explicitProfile, abspath)
}

// blUnload hot-reloads the bypass routes of the running daemon using only the
// raw -bl value, discarding the bl-file. Without arguments it auto-detects the
// TUN/RAW profile; for socks profiles, specify -p/--profile.
func blUnload(explicitProfile string) {
	sendBlReloadCommand(explicitProfile, "")
}

// blLoad hot-reloads the bypass routes file of the currently running qwdtt
// connection (tun, raw or socks) through the daemon's control socket. The new
// bl-file replaces the one used at connect time; -bl domains are merged in.
// No reconnect is required.
func blLoad(args []string, explicitProfile string) {
	if len(args) == 0 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "Usage: qwdtt bl load [-p|--profile PROFILE] <path>  (JSON с полем bypassRoutes)")
		os.Exit(1)
	}
	rawPath := args[0]

	// Validate up front, in the parent process, for friendly errors.
	if _, err := loadBypassRoutesFile(rawPath); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
	reloadBlFile(rawPath, explicitProfile)
}

func blList(file string) {
	_, domains, _, err := loadBypassForEdit(file)
	if err != nil {
		log.Fatalf("Ошибка чтения %q: %v", file, err)
	}
	if len(domains) == 0 {
		fmt.Printf("[*] В файле %q нет доменов\n", file)
	} else {
		sorted := append([]string(nil), domains...)
		sort.Slice(sorted, func(i, j int) bool {
			return strings.ToLower(displayDomain(sorted[i])) < strings.ToLower(displayDomain(sorted[j]))
		})
		fmt.Printf("Всего: %d доменов в %q\n\n", len(sorted), file)

		prof, d, found := blProfileForFile(file)
		pending := found && isBlPendingForProfile(prof, d)
		appliedSet := map[string]bool{}
		pureFile := found && d.BlackList == ""
		if found && len(d.BlackListDomains) > 0 {
			for _, ad := range d.BlackListDomains {
				appliedSet[domainKey(ad)] = true
			}
		}

		// Classify file domains against the applied set. Unapplied domains get
		// a pending-add prefix ("[!]"), which formatTaggedDomains renders as a
		// per-row marker in a separate left column (variant B2); applied
		// domains are listed plainly.
		type tagged = blDomain
		var taggedSorted []tagged
		var removed []string
		fileKeySet := map[string]bool{}
		hasPending := false
		for _, d2 := range sorted {
			k := domainKey(d2)
			fileKeySet[k] = true
			if _, applied := appliedSet[k]; applied {
				taggedSorted = append(taggedSorted, tagged{Domain: displayDomain(d2)})
			} else {
				taggedSorted = append(taggedSorted, tagged{Prefix: "[!]", Domain: displayDomain(d2)})
				hasPending = true
			}
		}
		if pureFile {
			for _, ad := range d.BlackListDomains {
				if !fileKeySet[domainKey(ad)] {
					removed = append(removed, ad)
					hasPending = true
				}
			}
		}

		if hasPending && len(appliedSet) > 0 {
			fmt.Print(formatTaggedDomains(taggedSorted, "", "  ", 3))
			if len(removed) > 0 {
				fmt.Printf("\n  Удалены, но не применены (bl rm без -r):\n")
				rmTags := make([]blDomain, 0, len(removed))
				for _, d2 := range d.BlackListDomains {
					if !fileKeySet[domainKey(d2)] {
						rmTags = append(rmTags, blDomain{Prefix: "[-]", Domain: displayDomain(d2)})
					}
				}
				fmt.Print(formatTaggedDomains(rmTags, "    ", "  ", 3))
			}
		} else {
			// No applied set or everything matches: plain list.
			displayed := make([]string, len(sorted))
			for i, d2 := range sorted {
				displayed[i] = displayDomain(d2)
			}
			fmt.Print(formatDomainsInColumns(displayed, "", "  ", 3))
		}

		if pending {
			fmt.Printf("\n[!] %s: изменения в bl-file не применены "+
				"(bl add/bl rm без -r). \nПримените: qwdtt bl load -p %s\n",
				prof, prof)
		}
	}
}

func validateBlDomains(domains []string) {
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if strings.Contains(d, ",") {
			fmt.Fprintf(os.Stderr, "[ERROR] домен не может содержать запятую: %q\n", d)
			os.Exit(1)
		}
	}
}

func blAdd(domains []string, file string, reload bool, profile string) {
	validateBlDomains(domains)
	raw, existing, isArray, err := loadBypassForEdit(file)
	createNew := false
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalf("Ошибка чтения %q: %v", file, err)
		}
		raw = map[string]json.RawMessage{}
		existing = []string{}
		isArray = false
		createNew = true
	}

	seen := make(map[string]bool, len(existing))
	for _, d := range existing {
		seen[strings.ToLower(d)] = true
	}

	added := 0
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		key := strings.ToLower(d)
		if seen[key] {
			fmt.Fprintf(os.Stderr, "[INFO] уже есть: %s\n", d)
			continue
		}
		seen[key] = true
		existing = append(existing, d)
		added++
		fmt.Printf("[+] добавлен: %s\n", d)
	}

	if added == 0 {
		fmt.Println("[*] Нечего добавлять — все домены уже присутствуют")
		return
	}

	if err := saveBypassFile(file, raw, existing, isArray, createNew); err != nil {
		log.Fatalf("Ошибка сохранения %q: %v", file, err)
	}
	fmt.Printf("\n[OK] Файл обновлён: %q (всего %d доменов)\n", file, len(existing))
	if reload {
		reloadBlFile(file, profile)
	}
}

func blRemove(domains []string, file string, assumeYes bool, reload bool, profile string) {
	validateBlDomains(domains)
	if !assumeYes {
		fmt.Printf("Точно удалить %d домен(а/ов) из %q? [y/N] ", len(domains), file)
		reader := bufio.NewReader(os.Stdin)
		resp, _ := reader.ReadString('\n')
		resp = strings.ToLower(strings.TrimSpace(resp))
		if resp != "y" && resp != "yes" {
			fmt.Println("[*] Отмена")
			return
		}
	}

	raw, existing, isArray, err := loadBypassForEdit(file)
	if err != nil {
		log.Fatalf("Ошибка чтения %q: %v", file, err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, d := range existing {
		existingSet[strings.ToLower(d)] = true
	}

	removeSet := make(map[string]bool)
	var reported []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		kl := strings.ToLower(d)
		if existingSet[kl] {
			removeSet[kl] = true
			reported = append(reported, d)
		} else {
			fmt.Fprintf(os.Stderr, "[INFO] не найден: %s\n", d)
		}
	}

	if len(removeSet) == 0 {
		fmt.Println("[*] Не удалено ни одного домена")
		return
	}

	var out []string
	for _, d := range existing {
		if removeSet[strings.ToLower(d)] {
			continue
		}
		out = append(out, d)
	}

	for _, d := range reported {
		fmt.Printf("[-] удалён: %s\n", d)
	}

	if err := saveBypassFile(file, raw, out, isArray, false); err != nil {
		log.Fatalf("Ошибка сохранения %q: %v", file, err)
	}
	fmt.Printf("\n[OK] Файл обновлён: %q (всего %d доменов)\n", file, len(out))
	if reload {
		reloadBlFile(file, profile)
	}
}

func blFind(queries []string, file string) {
	validateBlDomains(queries)
	_, existing, _, err := loadBypassForEdit(file)
	if err != nil {
		log.Fatalf("Ошибка чтения %q: %v", file, err)
	}
	foundAny := false
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		ql := strings.ToLower(q)
		var exact, match []string
		for _, d := range existing {
			dl := strings.ToLower(d)
			du := strings.ToLower(displayDomain(d))
			// dr is the romanized form of the Unicode domain, so a Latin
			// query (e.g. "vtb") also matches a Cyrillic domain
			// (e.g. "втб.рф" => "vtb.rf").
			dr := strings.ToLower(transliterateCyrillic(du))
			if dl == ql || du == ql || dr == ql {
				exact = append(exact, d)
			} else if strings.Contains(dl, ql) || strings.Contains(du, ql) || strings.Contains(dr, ql) {
				match = append(match, d)
			}
		}
		if len(exact) == 0 && len(match) == 0 {
			fmt.Printf("[NOTFOUND] %s\n", q)
			continue
		}
		foundAny = true
		for _, d := range exact {
			fmt.Printf("[EXACT]   %s -> %s\n", q, displayDomain(d))
		}
		for _, d := range match {
			fmt.Printf("[MATCH]   %s -> %s\n", q, displayDomain(d))
		}
	}
	if !foundAny {
		fmt.Println("[*] Ни одного совпадения не найдено")
	}
}

func subscriptionCmd() {
	printSubUsage := func() {
		fmt.Fprintln(os.Stderr, "Usage: qwdtt subscription <add|remove|show|move|update> [options] [names/urls]")
		fmt.Fprintln(os.Stderr, "  add [name] <url>, rm <name>, sh [name], mv <old> <new>, upd [names...]")
		fmt.Fprintln(os.Stderr, "  -y, --yes  Skip confirmation prompt")
		fmt.Fprintln(os.Stderr, "  (alias: qwdtt sub)")
	}
	if len(os.Args) < 3 {
		printSubUsage()
		os.Exit(1)
	}

	sub := ""
	for _, a := range os.Args[2:] {
		if a != "-" && !strings.HasPrefix(a, "-") {
			sub = a
			break
		}
	}
	if sub == "" {
		printSubUsage()
		os.Exit(1)
	}
	switch sub {
	case "add":
		subscriptionAddCmd()
	case "remove", "rm":
		subscriptionRemoveCmd()
	case "show", "sh":
		subscriptionShowCmd()
	case "move", "mv":
		subscriptionMoveCmd()
	case "update", "upd":
		subscriptionUpdateCmd()
	default:
		fmt.Fprintf(os.Stderr, "[ERROR] неизвестная подкоманда '%s'. Доступные: add, remove (rm), show (sh), move (mv), update\n", sub)
		os.Exit(1)
	}
}

// parseSubCmdFlags splits os.Args[2:] into parsed flags and positional args,
// skipping the subcommand name (first positional arg).
func parseSubCmdFlags(fs *flag.FlagSet) []string {
	flagArgs, args := splitFlagsAndArgs(fs, os.Args[2:])
	if len(args) > 0 {
		args = args[1:]
	}
	fs.Parse(flagArgs)
	return args
}

func subscriptionAddCmd() {
	fs := flag.NewFlagSet("subscription add", flag.ExitOnError)
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")

	args := parseSubCmdFlags(fs)

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt sub add <url> [-y]\n")
		fmt.Fprintf(os.Stderr, "       qwdtt sub add <name> <url> [-y]\n")
		fmt.Fprintf(os.Stderr, "  <name> — имя подписки (также название группы). Если не указано, берётся из JSON.\n")
		fmt.Fprintf(os.Stderr, "  <url>  — http(s):// ссылка на JSON подписки или Base64 с JSON\n")
		os.Exit(1)
	}

	var name, url string
	if len(args) == 1 {
		url = args[0]
	} else {
		name = args[0]
		url = args[1]
	}

	if name == "" {
		// Name from JSON — need to fetch first, then derive name
		data, err := fetchSubscriptionJSON(url)
		if err != nil {
			log.Fatalf("Ошибка получения подписки: %v", err)
		}

		parsed, err := parseSubscriptionJSON(data)
		if err != nil {
			log.Fatalf("Ошибка парсинга JSON: %v", err)
		}

		name = parsed.SubscriptionName
		if name == "" {
			log.Fatal("Ошибка: subscriptionName не найден в JSON. Укажите имя явно: qwdtt sub add <name> <url>")
		}

		fmt.Printf("[*] Подписка: %s\n", name)
		fmt.Printf("[*] URL: %s\n", url)
		fmt.Printf("[*] Профилей: %d\n", len(parsed.Profiles))

		if _, err := loadSubscription(name); err == nil {
			if !*yes {
				fmt.Printf("Подписка '%s' уже существует. Обновить? [y/N]: ", name)
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
		} else {
			if !*yes {
				fmt.Printf("Добавить подписку '%s'? [y/N]: ", name)
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
		}

		created, removed, err := applySubscription(name, url)
		if err != nil {
			log.Fatalf("Ошибка: %v", err)
		}

		printSubscriptionOpResult(name, url, created, removed, nil)
		return
	}

	// Explicit name — check if subscription already exists
	if existing, err := loadSubscription(name); err == nil {
		fmt.Printf("[*] Подписка '%s' уже существует (URL: %s)\n", name, existing.URL)
		if existing.URL == url {
			fmt.Println("[*] URL совпадает")
		} else {
			fmt.Printf("[*] URL изменён: %s → %s\n", existing.URL, url)
		}
		if !*yes {
			fmt.Printf("Обновить подписку '%s'? [y/N]: ", name)
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
	}

	created, removed, err := applySubscription(name, url)
	if err != nil {
		log.Fatalf("Ошибка: %v", err)
	}

	printSubscriptionOpResult(name, url, created, removed, nil)
}

func subscriptionRemoveCmd() {
	fs := flag.NewFlagSet("subscription remove", flag.ExitOnError)
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")

	args := parseSubCmdFlags(fs)

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt sub rm <name> [-y]\n")
		fmt.Fprintf(os.Stderr, "  name — имя подписки (и группы профилей)\n")
		os.Exit(1)
	}

	name := args[0]

	sub, err := loadSubscription(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' не найдена\n", name)
		fmt.Fprintf(os.Stderr, "Доступные подписки: %s\n", strings.Join(listSubscriptions(), ", "))
		os.Exit(1)
	}

	fmt.Printf("Удалить подписку '%s' и %d профилей?\n", name, len(sub.Profiles))
	for _, p := range sub.Profiles {
		fmt.Printf("  - %s\n", p)
	}

	if !*yes {
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

	var deleted int
	for _, p := range sub.Profiles {
		if err := os.Remove(profilePath(p)); err != nil {
			if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "[WARNING] Не удалось удалить профиль '%s': %v\n", p, err)
			}
			continue
		}
		deleted++
	}

	if err := os.Remove(subscriptionPath(name)); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось удалить конфиг подписки: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[OK] Подписка '%s' удалена (%d профилей удалено)\n", name, deleted)
}

func subscriptionMoveCmd() {
	fs := flag.NewFlagSet("subscription move", flag.ExitOnError)
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")

	positional := parseSubCmdFlags(fs)

	if len(positional) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: qwdtt sub mv <old_name> <new_name> [-y]\n")
		os.Exit(1)
	}

	oldName := positional[0]
	newName := positional[1]
	_ = yes

	if oldName == newName {
		fmt.Fprintf(os.Stderr, "[ERROR] Old and new names are the same\n")
		os.Exit(1)
	}

	if _, err := os.Stat(subscriptionPath(oldName)); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' не найдена\n", oldName)
		fmt.Fprintf(os.Stderr, "Доступные подписки: %s\n", strings.Join(listSubscriptions(), ", "))
		os.Exit(1)
	}

	if _, err := os.Stat(subscriptionPath(newName)); err == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' уже существует\n", newName)
		os.Exit(1)
	}

	if isSubscriptionName(newName) {
		fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' уже существует\n", newName)
		os.Exit(1)
	}

	sub, err := loadSubscription(oldName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось загрузить подписку '%s': %v\n", oldName, err)
		os.Exit(1)
	}

	sub.SubscriptionName = newName

	if err := os.MkdirAll(subscriptionDir(), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось создать директорию подписок: %v\n", err)
		os.Exit(1)
	}

	if err := saveSubscription(newName, sub); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось сохранить подписку '%s': %v\n", newName, err)
		os.Exit(1)
	}

	if err := os.Remove(subscriptionPath(oldName)); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Не удалось удалить старую подписку '%s': %v\n", oldName, err)
		os.Exit(1)
	}

	for _, profileName := range sub.Profiles {
		prof, err := loadProfile(profileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Не удалось обновить профиль '%s': %v\n", profileName, err)
			continue
		}
		prof.Subscription = newName
		for i, g := range prof.Groups {
			if g == oldName {
				prof.Groups[i] = newName
			}
		}
		if err := saveProfile(profileName, *prof); err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Не удалось сохранить профиль '%s': %v\n", profileName, err)
		}
	}

	fmt.Printf("[OK] Подписка '%s' переименована в '%s'\n", oldName, newName)
}

func subscriptionShowCmd() {
	fs := flag.NewFlagSet("subscription show", flag.ExitOnError)
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")

	args := parseSubCmdFlags(fs)
	_ = yes

	var name string
	if len(args) > 0 {
		name = args[0]
	}

	if name == "" {
		subs := listSubscriptions()
		if len(subs) == 0 {
			fmt.Println("Нет подписок")
			return
		}
		fmt.Println("Подписки:")
		for _, s := range subs {
			sub, err := loadSubscription(s)
			if err != nil {
				fmt.Printf("  %s [ошибка чтения]\n\n", s)
				continue
			}
			fmt.Printf("  %s\n", s)
			fmt.Printf("    Профилей: %d\n", len(sub.Profiles))
			if sub.LastFetch != "" {
				fmt.Printf("    Обновлено: %s\n", sub.LastFetch)
			}
			fmt.Printf("    URL:\n")
			fmt.Printf("      %s\n", sub.URL)
			fmt.Println()
		}
		return
	}

	sub, err := loadSubscription(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' не найдена\n", name)
		fmt.Fprintf(os.Stderr, "Доступные подписки: %s\n", strings.Join(listSubscriptions(), ", "))
		os.Exit(1)
	}

	fmt.Printf("Подписка: %s\n", sub.SubscriptionName)
	if sub.Description != "" {
		fmt.Printf("  Описание: %s\n", sub.Description)
	}
	if sub.TrafficUsedMb > 0 || sub.TrafficLimitMb > 0 {
		fmt.Printf("  Трафик: %.1f / %.1f MB\n", sub.TrafficUsedMb, sub.TrafficLimitMb)
	}
	if sub.UpdatedAt != "" {
		fmt.Printf("  Обновлено: %s\n", sub.UpdatedAt)
	}
	if sub.Version > 0 {
		fmt.Printf("  Версия: %d\n", sub.Version)
	}
	if sub.LastFetch != "" {
		fmt.Printf("  Последний fetch: %s\n", sub.LastFetch)
	}
	fmt.Printf("  Управляемых профилей: %d\n", len(sub.Profiles))

	if len(sub.Profiles) > 0 {
		fmt.Printf("\n  Профили:\n")
		maxLen := 0
		maxNumWidth := 0
		for _, p := range sub.Profiles {
			if len(p) > maxLen {
				maxLen = len(p)
			}
		}
		maxNumWidth = len(strconv.Itoa(len(sub.Profiles)))
		for i, p := range sub.Profiles {
			prof, err := loadProfile(p)
			num := i + 1
			if err != nil {
				fmt.Printf("    %*d. %-*s [не найден]\n", maxNumWidth, num, maxLen, p)
				continue
			}
			enabled := isProfileEnabled(p)
			status := "✓"
			if !enabled {
				status = "✗"
			}
			fmt.Printf("    %*d. %-*s [%s] %s\n", maxNumWidth, num, maxLen, p, status, prof.PeerAddr)
		}
	}
	fmt.Printf("  URL:\n    %s\n", sub.URL)
}

func subscriptionUpdateCmd() {
	fs := flag.NewFlagSet("subscription update", flag.ExitOnError)
	yes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.BoolVar(yes, "yes", false, "Skip confirmation prompt")

	args := parseSubCmdFlags(fs)

	var names []string
	if len(args) == 0 {
		names = listSubscriptions()
		if len(names) == 0 {
			fmt.Println("Нет подписок для обновления")
			return
		}
	} else {
		names = args
	}

	for _, name := range names {
		sub, err := loadSubscription(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Подписка '%s' не найдена\n", name)
			continue
		}

		if !*yes {
			fmt.Printf("Обновить подписку '%s' (URL: %s)? [y/N]: ", name, sub.URL)
			var answer string
			if _, err := fmt.Scanf("%s", &answer); err != nil {
				answer = ""
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Printf("[*] Пропущена: %s\n", name)
				continue
			}
		}

		fmt.Printf("[*] Обновление подписки '%s'...\n", name)
		created, removed, err := applySubscription(name, sub.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] %s: %v\n", name, err)
			continue
		}

		printSubscriptionOpResult(name, sub.URL, created, removed, sub)
	}
}

func printSubscriptionOpResult(name, url string, created, removed []string, sub *SubscriptionConfig) {
	fmt.Printf("\n[OK] Подписка '%s':\n", name)

	maxNameLen := 0
	maxPeerLen := 0
	for _, p := range created {
		if len(p) > maxNameLen {
			maxNameLen = len(p)
		}
		prof, err := loadProfile(p)
		if err == nil && len(prof.PeerAddr) > maxPeerLen {
			maxPeerLen = len(prof.PeerAddr)
		}
	}
	for _, p := range removed {
		if len(p) > maxNameLen {
			maxNameLen = len(p)
		}
	}

	for _, p := range created {
		prof, err := loadProfile(p)
		if err != nil {
			fmt.Printf("  + %-*s [ошибка загрузки]\n", maxNameLen, p)
		} else {
			status := "enabled"
			if !isProfileEnabled(p) {
				status = "disabled"
			}
			fmt.Printf("  + %-*s  %-*s  %s  [%s]\n", maxNameLen, p, maxPeerLen, prof.PeerAddr, maskPassword(prof.Password), status)
		}
	}
	for _, p := range removed {
		fmt.Printf("  - %s\n", p)
	}
	if len(created) == 0 && len(removed) == 0 {
		fmt.Println("  (без изменений)")
	}
	if sub != nil {
		if sub.Description != "" {
			fmt.Printf("  Описание: %s\n", sub.Description)
		}
		if sub.TrafficUsedMb > 0 || sub.TrafficLimitMb > 0 {
			fmt.Printf("  Трафик: %.1f / %.1f MB\n", sub.TrafficUsedMb, sub.TrafficLimitMb)
		}
	}
	fmt.Printf("  URL:\n    %s\n", url)
}
