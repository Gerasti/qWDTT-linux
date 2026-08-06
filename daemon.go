package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sevlyar/go-daemon"
)

func pidFilesDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "/tmp"
		} else {
			base = filepath.Join(home, ".local", "run")
		}
	}
	dir := filepath.Join(base, "qwdtt")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func pidFilePath(profile string) string {
	return filepath.Join(pidFilesDir(), "qwdtt-"+profile+".pid")
}

func socketPath(profile string) string {
	return filepath.Join(pidFilesDir(), "qwdtt-"+profile+".sock")
}

func logFilePath(profile string) string {
	return filepath.Join(pidFilesDir(), "qwdtt-"+profile+".log")
}

// listLogProfileNames returns profile names that have existing log files.
func listLogProfileNames() []string {
	dir := pidFilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".log") || !strings.HasPrefix(name, "qwdtt-") {
			continue
		}
		profile := strings.TrimSuffix(name, ".log")
		profile = strings.TrimPrefix(profile, "qwdtt-")
		if profile != "" {
			names = append(names, profile)
		}
	}
	return names
}

type DaemonManager struct {
	profile    string
	autoSwitch bool
	daemonCtx  *daemon.Context
}

func newDaemonManager(profile string, autoSwitch bool) *DaemonManager {
	dm := &DaemonManager{
		profile:    profile,
		autoSwitch: autoSwitch,
	}
	return dm
}

// daemonProfileName returns the name used for PID/log/socket files.
// In auto-switch mode, it always returns "autoswitch" to avoid conflicts
// with individual profile daemon processes.
func (dm *DaemonManager) daemonProfileName() string {
	if dm.autoSwitch {
		return "autoswitch"
	}
	return dm.profile
}

func (dm *DaemonManager) Start() (*os.Process, bool, error) {
	dm.daemonCtx = &daemon.Context{
		PidFileName: pidFilePath(dm.daemonProfileName()),
		PidFilePerm: 0o644,
		LogFileName: logFilePath(dm.daemonProfileName()),
		LogFilePerm: 0o644,
		WorkDir:     "/",
		Umask:       022,
		Args:        append([]string{filepath.Base(os.Args[0])}, os.Args[1:]...),
	}

	if daemon.WasReborn() {
		// We are in the child (daemon) process. Complete child-side setup.
		_, err := dm.daemonCtx.Reborn()
		if err != nil {
			return nil, false, fmt.Errorf("daemon child init: %w", err)
		}
		return nil, true, nil
	}

	child, err := dm.daemonCtx.Reborn()
	if err != nil {
		return nil, false, fmt.Errorf("failed to daemonize: %v", err)
	}

	if child != nil {
		// Parent process: child is the new daemon.
		return child, false, nil
	}

	return nil, true, nil
}

func (dm *DaemonManager) Release() {
	if dm.daemonCtx != nil {
		_ = dm.daemonCtx.Release()
	}
}

func (dm *DaemonManager) Stop() {
	if dm.daemonCtx != nil {
		proc, err := dm.daemonCtx.Search()
		if err == nil && proc != nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
}

func (dm *DaemonManager) Pid() (int, error) {
	pidStr, err := os.ReadFile(pidFilePath(dm.daemonProfileName()))
	if err != nil {
		return 0, fmt.Errorf("pid file not found: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidStr)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid: %w", err)
	}
	return pid, nil
}

func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func killProcess(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

func signalProcessGroup(pid int, sig syscall.Signal) error {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return err
	}
	return syscall.Kill(-pgid, sig)
}

func cleanupPidFile(profile string) {
	_ = os.Remove(pidFilePath(profile))
}

// isAutoSwitchDaemon checks if the current process is an autoswitch daemon
// by checking for the existence of the autoswitch PID file.
func isAutoSwitchDaemon() bool {
	pid, err := os.ReadFile(pidFilePath("autoswitch"))
	return err == nil && strings.TrimSpace(string(pid)) != ""
}

func initSignalHandler(done chan struct{}, sigCh chan os.Signal) {
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for sig := range sigCh {
			fmt.Printf("\n[*] Получен сигнал: %s\n", sig)
			switch sig {
			case syscall.SIGUSR1:
				fmt.Println("[*] Перезагрузка конфигурации...")
			case syscall.SIGUSR2:
				fmt.Println("[*] Переключение уровня логирования...")
			default:
				close(done)
				return
			}
		}
	}()
}
