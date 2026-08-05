package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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

		cmd := exec.Command("ps", "-p", pid, "-o", "%cpu,rss", "--no-headers")
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		fields := strings.Fields(string(output))
		if len(fields) < 2 {
			continue
		}

		cpuStr := strings.Replace(fields[0], ",", ".", -1)
		cpu, err := strconv.ParseFloat(cpuStr, 64)
		if err == nil {
			totalCPU += cpu
		}

		rss, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			totalMem += rss * 1024
			foundProcess = true
		}

		// Read thread count from /proc/<pid>/status
		statusData, err := os.ReadFile(fmt.Sprintf("/proc/%s/status", pid))
		if err == nil {
			for _, line := range strings.Split(string(statusData), "\n") {
				if strings.HasPrefix(line, "Threads:") {
					threadCount, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Threads:")))
					if err == nil {
						totalThreads += threadCount
					}
					break
				}
			}
		}
	}

	if !foundProcess {
		return nil, fmt.Errorf("no active connection process found")
	}

	// Workers ≈ threads - main thread - stdin reader goroutine - suspend monitor goroutine
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
