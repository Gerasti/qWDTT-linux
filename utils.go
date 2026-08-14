package main

import (
	"fmt"
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
		fmt.Fprintln(os.Stderr, "Используйте: qwdtt add <name> <wdtt://...>")
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

func getWGStats() (*WGStats, error) {
	data, err := os.ReadFile("/sys/class/net/" + wgIface + "/statistics/rx_bytes")
	if err != nil {
		return nil, fmt.Errorf("интерфейс не найден")
	}
	rxBytes, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + wgIface + "/statistics/tx_bytes")
	if err != nil {
		return nil, err
	}
	txBytes, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + wgIface + "/statistics/rx_packets")
	if err != nil {
		return nil, err
	}
	rxPackets, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)

	data, err = os.ReadFile("/sys/class/net/" + wgIface + "/statistics/tx_packets")
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
	CPU      float64
	Memory   int64
	Threads  int
	Workers  int
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
		CPU:      totalCPU,
		Memory:   totalMem,
		Threads:  totalThreads,
		Workers:  workers,
	}, nil
}

// ProfileDetails holds information about a running qwdtt profile process.
type ProfileDetails struct {
	Mode          string // "tun" or "socks"
	SocksPort     int    // SOCKS5 port (only relevant for socks mode)
	PID           int    // Process PID
	BlackList     string // raw -bl / --black-list value (comma-separated domains)
	BlackListFile string // path to -bl-file / --black-list-file JSON file
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
		} else if strings.Contains(cmdline, "-mode raw") || strings.Contains(cmdline, "--mode=raw") ||
			strings.Contains(cmdline, "-mode=raw") {
			d.Mode = "raw"
		}

		// Parse -bl / --black-list and -bl-file / --black-list-file
		fields := strings.Fields(cmdline)
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
		if bl, blFile := readSplitCfg(profile); bl != "" || blFile != "" {
			d.BlackList = bl
			d.BlackListFile = blFile
		}

		details[profile] = d
	}

	return details
}

