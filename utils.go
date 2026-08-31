package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// defaultSocksPort is the default SOCKS5 port used when -socks-port is not specified.
const defaultSocksPort = 9050

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

func selectProfileInteractive() string {
	type profileEntry struct {
		name string
		peer string
	}

	var profiles []profileEntry

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
			if !isProfileEnabled(name) {
				continue
			}
			profiles = append(profiles, profileEntry{name: name, peer: prof.PeerAddr})
		}
	}

	readFromDir(profilesDir())
	readFromDir(filepath.Join(configDir(), "ro-profiles"))

	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr, "Нет включенных профилей")
		fmt.Fprintln(os.Stderr, "Используйте: qwdtt add <name> <wdtt://...|qwdtt://config?name=...>")
		fmt.Fprintln(os.Stderr, "Или: qwdtt enable <name>")
		return ""
	}

	fmt.Println("Выберите профиль:")
	for i, p := range profiles {
		fmt.Printf("  %d. %s (%s)\n", i+1, p.name, p.peer)
	}
	fmt.Print("> ")

	var choice int
	if _, err := fmt.Scanf("%d", &choice); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка ввода")
		return ""
	}

	if choice < 1 || choice > len(profiles) {
		fmt.Fprintln(os.Stderr, "Неверный выбор")
		return ""
	}

	return profiles[choice-1].name
}

type WGStats struct {
	RxBytes   int64
	TxBytes   int64
	RxPackets int64
	TxPackets int64
}

func getWGStats(iface string) (*WGStats, error) {
	data, err := os.ReadFile("/sys/class/net/" + iface + "/statistics/rx_bytes")
	if err != nil {
		return nil, fmt.Errorf("интерфейс TUN/RAW не найден")
	}
	rxBytes, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + iface + "/statistics/tx_bytes")
	if err != nil {
		return nil, err
	}
	txBytes, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + iface + "/statistics/rx_packets")
	if err != nil {
		return nil, err
	}
	rxPackets, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + iface + "/statistics/tx_packets")
	if err != nil {
		return nil, err
	}
	txPackets, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	return &WGStats{
		RxBytes:   rxBytes,
		TxBytes:   txBytes,
		RxPackets: rxPackets,
		TxPackets: txPackets,
	}, nil
}

type ProcessUsage struct {
	CPU     float64
	Memory  int64
	Threads int
	Workers int
}

// getProcessUsageByPID returns resource usage for a specific PID.
func getProcessUsageByPID(pid int) (*ProcessUsage, error) {
	pidStr := strconv.Itoa(pid)
	cmd := exec.Command("ps", "-p", pidStr, "-o", "%cpu,rss", "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("process not found: %w", err)
	}

	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return nil, fmt.Errorf("cannot parse ps output for pid %d", pid)
	}

	cpuStr := strings.Replace(fields[0], ",", ".", -1)
	cpu, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse CPU for pid %d: %w", pid, err)
	}

	rss, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse memory for pid %d: %w", pid, err)
	}

	usage := &ProcessUsage{
		CPU:    cpu,
		Memory: rss * 1024,
	}

	// Read thread count from /proc/<pid>/status
	statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err == nil {
		for _, line := range strings.Split(string(statusData), "\n") {
			if strings.HasPrefix(line, "Threads:") {
				threadCount, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Threads:")))
				if err == nil {
					usage.Threads = threadCount
					if threadCount > 3 {
						usage.Workers = threadCount - 3
					}
				}
				break
			}
		}
	}

	return usage, nil
}

func getProcessUsage() (*ProcessUsage, error) {
	var pids []string

	// Primary: find PID via pidfile from active profile
	activeProfile := getActiveProfile()
	if activeProfile != "" {
		if data, err := os.ReadFile(pidFilePath(activeProfile)); err == nil {
			pidStr := strings.TrimSpace(string(data))
			if pidStr != "" {
				pids = []string{pidStr}
			}
		}
	}

	// Fallback: pgrep all qwdtt processes
	if len(pids) == 0 {
		cmd := exec.Command("pgrep", "-f", "qwdtt")
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("process not found")
		}
		pids = strings.Split(strings.TrimSpace(string(output)), "\n")
	}

	if len(pids) == 0 {
		return nil, fmt.Errorf("process not found")
	}

	selfPID := os.Getpid()
	var totalCPU float64
	var totalMem int64
	var totalThreads int
	foundProcess := false

	for _, pid := range pids {
		if pid == "" {
			continue
		}

		pidInt, _ := strconv.Atoi(pid)
		if pidInt == selfPID {
			continue
		}

		usage, err := getProcessUsageByPID(pidInt)
		if err != nil {
			continue
		}

		totalCPU += usage.CPU
		totalMem += usage.Memory
		totalThreads += usage.Threads
		foundProcess = true
	}

	if !foundProcess {
		return nil, fmt.Errorf("no active connection process found")
	}

	workers := 0
	if totalThreads > 3 {
		workers = totalThreads - 3
	}

	return &ProcessUsage{
		CPU:     totalCPU,
		Memory:  totalMem,
		Threads: totalThreads,
		Workers: workers,
	}, nil
}

// ProfileDetails holds information about a running qwdtt profile process.
type ProfileDetails struct {
	Mode             string   // "tun" or "socks"
	SocksPort        int      // SOCKS5 port (only relevant for socks mode)
	SocksUser        string   // SOCKS5 username (if specified via -socks-user)
	SocksPass        string   // SOCKS5 password (if specified via -socks-password)
	SocksPublic      bool     // SOCKS5 listening on 0.0.0.0 (via -pub/--public)
	SocksListenAddr  string   // SOCKS5 bind address (via -listen)
	PID              int      // Process PID
	BlackList        string   // raw -bl / --black-list value (space-separated domains)
	BlackListFile    string   // path to -bl-file / --black-list-file JSON file
	BlackListMtime   int64    // mtime (UnixNano) of bl-file at last apply; >0 if file-based
	BlackListDomains []string // domains applied from -bl + bl-file at last apply
	SplitRoutes      []string // resolved bypass route CIDRs (from state file)
}

// getRunningProfileDetails returns details (mode, socks port, PID) for each running profile.
func getRunningProfileDetails() map[string]*ProfileDetails {
	details := make(map[string]*ProfileDetails)

	// Use PID files first for reliable discovery
	entries, err := os.ReadDir(pidFilesDir())
	if err != nil {
		return details
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".pid") || !strings.HasPrefix(name, "qwdtt-") {
			continue
		}
		profile := strings.TrimSuffix(strings.TrimPrefix(name, "qwdtt-"), ".pid")
		if profile == "" {
			continue
		}

		pidStr := readPidFile(pidFilePath(profile))
		pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
		if err != nil || !isProcessAlive(pid) {
			continue
		}

		// Parse cmdline for mode and socks-port
		cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")

		d := &ProfileDetails{PID: pid, Mode: "tun"}
		// Check for mode
		if strings.Contains(cmdline, "-mode socks") || strings.Contains(cmdline, "--mode=socks") ||
			strings.Contains(cmdline, "-mode=socks") {
			d.Mode = "socks"
			// Default SOCKS port is 9050
			d.SocksPort = defaultSocksPort
			// Extract socks port if explicitly specified
			fields := strings.Fields(cmdline)
			for i, field := range fields {
				if (field == "-socks-port" || field == "--socks-port") && i+1 < len(fields) {
					port, err := strconv.Atoi(fields[i+1])
					if err == nil {
						d.SocksPort = port
					}
					break
				}
				if strings.HasPrefix(field, "-socks-port=") {
					port, err := strconv.Atoi(strings.TrimPrefix(field, "-socks-port="))
					if err == nil {
						d.SocksPort = port
					}
					break
				}
				if strings.HasPrefix(field, "--socks-port=") {
					port, err := strconv.Atoi(strings.TrimPrefix(field, "--socks-port="))
					if err == nil {
						d.SocksPort = port
					}
					break
				}
			}
			// Check for --pub / --public flag
			for _, field := range fields {
				if field == "-pub" || field == "--pub" || field == "-public" || field == "--public" {
					d.SocksPublic = true
					break
				}
			}
			// Check for --listen / -listen flag (value can be space-separated or =join)
			for i, field := range fields {
				if (field == "-listen" || field == "--listen") && i+1 < len(fields) {
					d.SocksListenAddr = fields[i+1]
					break
				}
				if strings.HasPrefix(field, "--listen=") {
					d.SocksListenAddr = strings.TrimPrefix(field, "--listen=")
					break
				}
				if strings.HasPrefix(field, "-listen=") {
					d.SocksListenAddr = strings.TrimPrefix(field, "-listen=")
					break
				}
			}
		} else if strings.Contains(cmdline, "-mode raw") || strings.Contains(cmdline, "--mode=raw") ||
			strings.Contains(cmdline, "-mode=raw") {
			d.Mode = "raw"
		}

		// Parse -bl / --black-list and -bl-file / --black-list-file
		fields := strings.Fields(cmdline)

		// Parse -socks-user / -socks-password
		for i, field := range fields {
			if (field == "-socks-user" || field == "--socks-user") && i+1 < len(fields) {
				d.SocksUser = fields[i+1]
			}
			if strings.HasPrefix(field, "-socks-user=") {
				d.SocksUser = strings.TrimPrefix(field, "-socks-user=")
			}
			if strings.HasPrefix(field, "--socks-user=") {
				d.SocksUser = strings.TrimPrefix(field, "--socks-user=")
			}
			if (field == "-socks-password" || field == "--socks-password") && i+1 < len(fields) {
				d.SocksPass = fields[i+1]
			}
			if strings.HasPrefix(field, "-socks-password=") {
				d.SocksPass = strings.TrimPrefix(field, "-socks-password=")
			}
			if strings.HasPrefix(field, "--socks-password=") {
				d.SocksPass = strings.TrimPrefix(field, "--socks-password=")
			}
		}

		for i, field := range fields {
			if (field == "-bl" || field == "--bl" || field == "--black-list") && i+1 < len(fields) {
				d.BlackList = fields[i+1]
			}
			if strings.HasPrefix(field, "-bl=") {
				d.BlackList = strings.TrimPrefix(field, "-bl=")
			}
			if strings.HasPrefix(field, "--bl=") {
				d.BlackList = strings.TrimPrefix(field, "--bl=")
			}
			if strings.HasPrefix(field, "--black-list=") {
				d.BlackList = strings.TrimPrefix(field, "--black-list=")
			}
			if (field == "-bl-file" || field == "--bl-file" || field == "--black-list-file") && i+1 < len(fields) {
				d.BlackListFile = fields[i+1]
			}
			if strings.HasPrefix(field, "-bl-file=") {
				d.BlackListFile = strings.TrimPrefix(field, "-bl-file=")
			}
			if strings.HasPrefix(field, "--bl-file=") {
				d.BlackListFile = strings.TrimPrefix(field, "--bl-file=")
			}
			if strings.HasPrefix(field, "--black-list-file=") {
				d.BlackListFile = strings.TrimPrefix(field, "--black-list-file=")
			}
		}

		// Prefer state file over cmdline parsing for blacklist info
		if bl, blFile, routes, mtime, domains := readSplitCfgFull(profile); bl != "" || blFile != "" || len(routes) > 0 {
			d.BlackList = bl
			d.BlackListFile = blFile
			d.BlackListMtime = mtime
			d.BlackListDomains = domains
			d.SplitRoutes = routes
		}

		details[profile] = d
	}

	// Handle autoswitch: the currently active profile runs under the autoswitch process
	currentProfile := getAutoswitchCurrentProfile()
	if currentProfile != "" {
		// Read autoswitch PID
		autoswitchPidStr := readPidFile(pidFilePath("autoswitch"))
		autoswitchPid, err := strconv.Atoi(strings.TrimSpace(autoswitchPidStr))
		if err == nil && isProcessAlive(autoswitchPid) {
			if _, exists := details[currentProfile]; !exists {
				// Try to parse cmdline of the autoswitch process for mode/socks-port
				cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", autoswitchPid)
				cmdlineData, err := os.ReadFile(cmdlinePath)
				mode := "tun"
				socksPort := 0
				socksPublic := false
				socksListenAddr := ""
				if err == nil {
					cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
					if strings.Contains(cmdline, "-mode socks") || strings.Contains(cmdline, "--mode=socks") ||
						strings.Contains(cmdline, "-mode=socks") {
						mode = "socks"
						socksPort = defaultSocksPort
						fields := strings.Fields(cmdline)
						for i, field := range fields {
							if (field == "-socks-port" || field == "--socks-port") && i+1 < len(fields) {
								port, pErr := strconv.Atoi(fields[i+1])
								if pErr == nil {
									socksPort = port
								}
								break
							}
							if strings.HasPrefix(field, "-socks-port=") {
								port, pErr := strconv.Atoi(strings.TrimPrefix(field, "-socks-port="))
								if pErr == nil {
									socksPort = port
								}
								break
							}
							if strings.HasPrefix(field, "--socks-port=") {
								port, pErr := strconv.Atoi(strings.TrimPrefix(field, "--socks-port="))
								if pErr == nil {
									socksPort = port
								}
								break
							}
						}
						// Check for --pub / --public flag
						for _, field := range fields {
							if field == "-pub" || field == "--pub" || field == "-public" || field == "--public" {
								socksPublic = true
								break
							}
						}
						// Check for --listen / -listen flag
						for i, field := range fields {
							if (field == "-listen" || field == "--listen") && i+1 < len(fields) {
								socksListenAddr = fields[i+1]
								break
							}
							if strings.HasPrefix(field, "--listen=") {
								socksListenAddr = strings.TrimPrefix(field, "--listen=")
								break
							}
							if strings.HasPrefix(field, "-listen=") {
								socksListenAddr = strings.TrimPrefix(field, "-listen=")
								break
							}
						}
					} else if strings.Contains(cmdline, "-mode raw") || strings.Contains(cmdline, "--mode=raw") ||
						strings.Contains(cmdline, "-mode=raw") {
						mode = "raw"
					}
				}
				d := &ProfileDetails{
					Mode:            mode,
					SocksPort:       socksPort,
					SocksPublic:     socksPublic,
					SocksListenAddr: socksListenAddr,
					PID:             autoswitchPid,
				}
				// Autoswitch does not have its own split-cfg; it lives on the
				// *current* profile (e.g. qwdtt-tp-s.bl). Mirror the per-profile
				// loop so `qwdtt debug` / `qwdtt bl -c` reflect a hot-reloaded
				// bl-file (qwdtt bl load).
				if bl, blFile, routes, mtime, domains := readSplitCfgFull(currentProfile); bl != "" || blFile != "" || len(routes) > 0 {
					d.BlackList = bl
					d.BlackListFile = blFile
					d.BlackListMtime = mtime
					d.BlackListDomains = domains
					d.SplitRoutes = routes
				}
				details[currentProfile] = d
			}
		}
	}

	return details
}

// getBlFileForProfile returns the bl-file path for the given running profile,
// reading it from the profile's state file (qwdtt-<profile>.bl). It works
// for any running profile — including socks connections, which don't set
// the active_profile file.
func getBlFileForProfile(profile string) string {
	if profile == "" {
		return getCurrentBlFile()
	}
	details := getRunningProfileDetails()
	if d, ok := details[profile]; ok && d.BlackListFile != "" {
		return d.BlackListFile
	}
	// Fallback: read the state file directly (covers profiles whose details
	// didn't populate BlackListFile, e.g. a plain tun/raw profile).
	if bl, blFile, _, _, _ := readSplitCfgFull(profile); blFile != "" {
		return blFile
	} else if bl != "" {
		// -bl was used without a file; nothing file-based to list.
		return ""
	}
	for _, d := range details {
		if d.BlackListFile != "" {
			return d.BlackListFile
		}
	}
	return ""
}

// getCurrentBlFile returns the bl-file path from the currently active profile.
// getAutoswitchMode returns the connection mode of the running autoswitch daemon
// by parsing its process cmdline. Returns "tun", "raw", or "socks".
// Returns "tun" as default if the PID is alive but cmdline cannot be parsed.
func getAutoswitchMode() string {
	pidStr := readPidFile(pidFilePath("autoswitch"))
	if pidStr == "" {
		return ""
	}
	pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	if err != nil || !isProcessAlive(pid) {
		return ""
	}
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	cmdlineData, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return "tun"
	}
	cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
	if strings.Contains(cmdline, "-mode socks") || strings.Contains(cmdline, "--mode=socks") ||
		strings.Contains(cmdline, "-mode=socks") {
		return "socks"
	}
	if strings.Contains(cmdline, "-mode raw") || strings.Contains(cmdline, "--mode=raw") ||
		strings.Contains(cmdline, "-mode=raw") {
		return "raw"
	}
	return "tun"
}

// getCurrentBlFile returns the bl-file path from the currently active tun/raw
// profile. It mirrors resolveBlSocket's no-`-p` selection so that `qwdtt bl
// ls`/`bl list` (auto-detect) and `qwdtt bl load <file>` (без -p) согласуются.
// socks-профили (в т.ч. autoswitch-current в socks) НЕ используются — их можно
// несколько, и bl без аргументов работает только с tun/raw (kernel interface).
func getCurrentBlFile() string {
	details := getRunningProfileDetails()

	// Priority: autoswitch current (если tun/raw) -> active_profile (если tun/raw) -> any running tun/raw.
	if isDaemonRunning("autoswitch") {
		if cur := getAutoswitchCurrentProfile(); cur != "" {
			if d, ok := details[cur]; ok && (d.Mode == "tun" || d.Mode == "raw") {
				return d.BlackListFile // may be "" if profile started without -bl-file
			}
		}
	}
	if active := getActiveProfile(); active != "" && active != "autoswitch" {
		if d, ok := details[active]; ok && (d.Mode == "tun" || d.Mode == "raw") {
			return d.BlackListFile // may be "" if profile started without -bl-file
		}
	}
	for _, d := range details {
		if d.Mode == "tun" || d.Mode == "raw" {
			return d.BlackListFile // may be "" if profile started without -bl-file
		}
	}
	return ""
}

// validateSocksListenAddr проверяет, что addr — валидный IP-адрес, пригодный
// для привязки SOCKS5-сервера. Отклоняет:
//   - не-IP строки;
//   - сетевые (network) и широковещательные (broadcast) адреса локальных подсетей;
//
// 0.0.0.0, loopback и host-адреса (/32, /128, /31, /127) допускаются.
func validateSocksListenAddr(addr string) error {
	if addr == "" {
		return nil
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("'%s' не является валидным IP-адресом", addr)
	}

	// 0.0.0.0 — слушать все интерфейсы (аналог --public)
	if ip.IsUnspecified() {
		return nil
	}

	// 127.x.x.x — loopback, разрешён
	if ip.IsLoopback() {
		return nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		// Не удалось перечислить интерфейсы — не будем блокировать
		return nil
	}

	found := false
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			switch v := a.(type) {
			case *net.IPNet:
				if ip.Equal(v.IP) {
					found = true
				}
			case *net.IPAddr:
				if ip.Equal(v.IP) {
					found = true
				}
			}

			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}

			ones, bits := ipNet.Mask.Size()
			// /32 (IPv4) или /128 (IPv6) — host-адрес, допустим
			if ones == bits {
				continue
			}
			// /31 (IPv4) или /127 (IPv6) — p2p, оба адреса пригодны
			if ones == bits-1 {
				continue
			}

			// Сетевой адрес подсети: маскируем ipNet.IP, чтобы получить network address
			networkAddr := ipNet.IP.Mask(ipNet.Mask)
			if ip.Equal(networkAddr) {
				return fmt.Errorf("'%s' — это сетевой (network) адрес подсети %s на интерфейсе %s", addr, ipNet.String(), iface.Name)
			}

			// Широковещательный адрес (IPv4 только)
			if v4 := ip.To4(); v4 != nil {
				if v4Net := networkAddr.To4(); v4Net != nil {
					networkInt := binary.BigEndian.Uint32(v4Net)
					maskInt := binary.BigEndian.Uint32(ipNet.Mask)
					broadcastInt := networkInt | ^maskInt
					broadcastIP := make(net.IP, 4)
					binary.BigEndian.PutUint32(broadcastIP, broadcastInt)
					if ip.Equal(broadcastIP) {
						return fmt.Errorf("'%s' — это широковещательный (broadcast) адрес подсети %s на интерфейсе %s", addr, ipNet.String(), iface.Name)
					}
				}
			}
		}
	}

	if !found {
		return fmt.Errorf("'%s' не назначен ни одному локальному интерфейсу", addr)
	}

	return nil
}

// isPublicSocksBind reports whether the given SOCKS listen address is
// bound to all interfaces (0.0.0.0 or ::), i.e. reachable from outside.
// It returns false for loopback (127.0.0.1, ::1), specific LAN IPs
// (e.g. 192.168.1.5), empty, or invalid strings.
func isPublicSocksBind(addr string) bool {
	if addr == "" {
		return false
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsUnspecified()
}
