package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"qwdtt/internal/core"

	"github.com/vishvananda/netlink"
)

// isDaemonRunning checks if a daemon process for the given profile daemon
// (e.g. "autoswitch" or a specific profile) is currently running via its PID file.
func isDaemonRunning(profile string) bool {
	pidStr, err := os.ReadFile(pidFilePath(profile))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidStr)))
	if err != nil {
		return false
	}
	return isProcessAlive(pid)
}

// isActiveProfile checks if the given profile is currently marked as the active profile.
func isActiveProfile(profile string) bool {
	return getActiveProfile() == profile
}

// isKernelInterfaceActive checks if the WireGuard tun interface (wg-qwdtt) is up.
func isKernelInterfaceActive() bool {
	_, err := netlink.LinkByName(wgIface)
	return err == nil
}

// isSocksPortInUse checks if the given SOCKS port is already in use.
func isSocksPortInUse(port int) bool {
	conn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

// switchProfileSignal is a thread-safe, resettable signal channel.
// It can be closed (triggered) to signal a profile switch, and then
// automatically reset for the next profile.
type switchProfileSignal struct {
	mu sync.Mutex
	ch chan struct{}
}

func newSwitchProfileSignal() *switchProfileSignal {
	return &switchProfileSignal{ch: make(chan struct{})}
}

func (s *switchProfileSignal) get() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ch
}

func (s *switchProfileSignal) trigger() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.ch)
	s.ch = make(chan struct{})
}

func connectCmd() {
	var profileName string

	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	workers := fs.Int("workers", 9, "Number of workers")
	mtu := fs.Int("mtu", 1280, "Tunnel MTU")
	hashes := fs.String("hashes", "", "VK hashes (comma-separated)")
	autoSwitch := fs.Bool("auto-switch", false, "Auto-switch to other profiles on failure")
	timeout := fs.Int("timeout", 120, "Connection timeout in seconds (for -auto-switch)")
	dns := fs.String("dns", "yandex", "DNS resolver (yandex|cloudflare|google|doh-yandex|doh-cloudflare|doh-google|custom:IP:PORT|doh:https://...)")
	captcha := fs.String("captcha", "auto", "Captcha bypass mode (auto|rjs)")
	mode := fs.String("mode", "tun", "Connection mode (tun|socks|raw)")
	socksPort := fs.Int("socks-port", defaultSocksPort, "SOCKS5 port (only with -mode socks)")
	rawPort := fs.Int("raw-port", 56003, "Server raw port (only with -mode raw)")
	transport := fs.String("transport", "udp", "Transport to TURN relay: udp or tcp (default udp). Use tcp where UDP is blocked")
	logFlag := fs.Bool("log", false, "Показывать лог демона в терминале в реальном времени")
	autoStop := fs.Bool("toggle", false, "Stop running profile, or start if not running")
	blackList := fs.String("black-list", "", "Black-list domains: these go direct (rest through tunnel). Comma-separated. TUN mode only")
	fs.StringVar(blackList, "bl", "", "Alias for --black-list")
	blackListFile := fs.String("black-list-file", "", "Read black-list domains from JSON file (bypassRoutes field). Can combine with -bl. TUN mode only")
	fs.StringVar(blackListFile, "bl-file", "", "Alias for --black-list-file")
	socksUser := fs.String("socks-user", "", "SOCKS5 username (only with -mode socks)")
	socksPass := fs.String("socks-password", "", "SOCKS5 password (only with -mode socks)")

	if len(os.Args) < 3 || strings.HasPrefix(os.Args[2], "-") {
		fs.Parse(os.Args[2:])

		if !*autoSwitch {
			profileName = selectProfileInteractive()
			if profileName == "" {
				os.Exit(1)
			}
		}
	} else {
		profileName = os.Args[2]
		fs.Parse(os.Args[3:])
	}

	daemonProfile := profileName
	if daemonProfile == "" && *autoSwitch {
		daemonProfile = "autoswitch"
	}
	if daemonProfile == "" {
		log.Fatal("Профиль не указан и не удалось определить")
	}

	// --- Split tunneling validation (before daemonizing) ---
	var splitCfg *splitTunnelConfig
	if *blackList != "" || *blackListFile != "" {
		if *mode != "tun" && *mode != "raw" && *mode != "socks" {
			fmt.Fprintln(os.Stderr, "[ERROR] Split tunneling доступен только в режимах -mode tun, -mode raw или -mode socks")
			os.Exit(1)
		}
		if *blackListFile != "" {
			if !filepath.IsAbs(*blackListFile) {
				if abs, err := filepath.Abs(*blackListFile); err == nil {
					*blackListFile = abs
				}
			}
			for i, arg := range os.Args {
				if (arg == "--bl-file" || arg == "-bl-file") && i+1 < len(os.Args) {
					os.Args[i+1] = *blackListFile
					break
				}
				if strings.HasPrefix(arg, "--bl-file=") {
					os.Args[i] = "--bl-file=" + *blackListFile
					break
				}
				if strings.HasPrefix(arg, "-bl-file=") {
					os.Args[i] = "-bl-file=" + *blackListFile
					break
				}
			}
		}
		blDomains := splitDomains(*blackList)
		if *blackListFile != "" {
			fileDomains, err := loadBypassRoutesFile(*blackListFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
				os.Exit(1)
			}
			blDomains = append(blDomains, fileDomains...)
		}
		splitCfg = &splitTunnelConfig{mode: "blacklist", domains: blDomains, rawBl: *blackList, rawFile: *blackListFile}
	}

	// Validate profile exists before daemonizing so we can report errors cleanly
	if !*autoSwitch {
		if _, err := loadProfile(daemonProfile); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не найден: %v\n", daemonProfile, err)
			os.Exit(1)
		}
	}

	// --- Pre-connection conflict checks (parent process) ---
	// These checks run before daemonization so we can use notify/fmt freely.

	// Handle --toggle: if the target profile is already running, stop it
	// and exit (don't connect). If not running, fall through to normal connect.
	if *autoStop {
		if *autoSwitch && isDaemonRunning("autoswitch") {
			fmt.Printf("[*] Остановка авто-переключения...\n")
			if !killByPidFile("autoswitch") {
				killByPgrep("autoswitch")
			}
			if activeProfile := getActiveProfile(); activeProfile == "autoswitch" {
				clearAutoswitchCurrentProfile()
				clearActiveProfile()
			}
			if isKernelInterfaceActive() {
				if err := teardownWG(); err == nil {
					fmt.Println("[OK] WireGuard конфиг удален")
				}
			}
			if cur := getAutoswitchCurrentProfile(); cur != "" {
				removeSplitCfg(cur)
			}
			fmt.Println("[OK] Авто-переключение остановлено")
			os.Exit(0)
		} else if !*autoSwitch && isDaemonRunning(daemonProfile) {
			fmt.Printf("[*] Остановка профиля '%s'...\n", daemonProfile)
			if !killByPidFile(daemonProfile) {
				fmt.Printf("[*] Pid файл не найден, fallback на pgrep...\n")
				killByPgrep(daemonProfile)
			}
			removeSplitCfg(daemonProfile)
			activeProfile := getActiveProfile()
			if activeProfile == daemonProfile {
				if err := teardownWG(); err == nil {
					fmt.Println("[OK] WireGuard конфиг удален")
				}
				clearActiveProfile()
			}
			notifyDisconnectedSync(daemonProfile, *mode)
			fmt.Printf("[OK] Профиль '%s' остановлен\n", daemonProfile)
			os.Exit(0)
		}
	}

	if *autoSwitch {
		// Check if autoswitch daemon is already running
		if isDaemonRunning("autoswitch") {
			msg := "Авто-переключение уже запущено (PID file: qwdtt-autoswitch.pid). Используйте qwdtt discon для остановки."
			fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
			notifyError("autoswitch", msg)
			os.Exit(1)
		}
		if *mode == "tun" && isKernelInterfaceActive() {
			activeProfile := getActiveProfile()
			target := activeProfile
			if target == "" {
				target = "другой процесс"
			}
			msg := fmt.Sprintf("Интерфейс wg-qwdtt уже используется (профиль: %s). Используйте qwdtt discon для остановки.", target)
			fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
			notifyError("autoswitch", msg)
			os.Exit(1)
		}
		if *mode == "tun" && rawIfaceActive() {
			activeProfile := getActiveProfile()
			target := activeProfile
			if target == "" {
				target = "другой процесс"
			}
			msg := fmt.Sprintf("Интерфейс %s уже используется в raw-режиме (профиль: %s). Используйте qwdtt discon для остановки.", core.RawIfaceName, target)
			fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
			notifyError("autoswitch", msg)
			os.Exit(1)
		}
	} else {
		// Single profile mode: check for conflicts
		if isDaemonRunning(daemonProfile) {
			msg := fmt.Sprintf("Профиль '%s' уже запущен (PID file: qwdtt-%s.pid). Используйте qwdtt discon %s для остановки.", daemonProfile, daemonProfile, daemonProfile)
			fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
			notifyError(profileName, msg)
			os.Exit(1)
		}
		if *mode == "tun" {
			// Kernel mode: only one connection allowed (wg-qwdtt interface).
			// Block if autoswitch daemon is running (it uses tun mode),
			// or if the tun interface is already active.
			if isDaemonRunning("autoswitch") {
				msg := "Уже запущено авто-переключение в режиме tun. Используйте qwdtt discon для остановки."
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
			if isKernelInterfaceActive() {
				activeProfile := getActiveProfile()
				target := activeProfile
				if target == "" {
					target = "другой процесс"
				}
				msg := fmt.Sprintf("Интерфейс wg-qwdtt уже используется (профиль: %s). Используйте qwdtt discon для остановки.", target)
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
			if rawIfaceActive() {
				activeProfile := getActiveProfile()
				target := activeProfile
				if target == "" {
					target = "другой процесс"
				}
				msg := fmt.Sprintf("Интерфейс %s уже используется в raw-режиме (профиль: %s). Используйте qwdtt discon для остановки.", core.RawIfaceName, target)
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
		} else if *mode == "raw" {
			if rawIfaceActive() {
				activeProfile := getActiveProfile()
				target := activeProfile
				if target == "" {
					target = "другой процесс"
				}
				msg := fmt.Sprintf("Интерфейс %s уже используется (профиль: %s). Используйте qwdtt discon для остановки.", core.RawIfaceName, target)
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
			if isKernelInterfaceActive() {
				activeProfile := getActiveProfile()
				target := activeProfile
				if target == "" {
					target = "другой процесс"
				}
				msg := fmt.Sprintf("Интерфейс wg-qwdtt уже используется в tun-режиме (профиль: %s). Используйте qwdtt discon для остановки.", target)
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
		} else if *mode == "socks" {
			// SOCKS mode: multiple connections allowed with different ports.
			// Do not block on tun interface (socks mode does not conflict with it),
			// as each socks instance uses a unique DTLS port (Listen=127.0.0.1:0, different from tun's 9000).
			if isSocksPortInUse(*socksPort) {
				msg := fmt.Sprintf("Порт SOCKS5 %d уже используется другим соединением. Укажите другой порт через -socks-port PORT.", *socksPort)
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
		}
	}

	dm := newDaemonManager(daemonProfile, *autoSwitch)
	_, isChild, err := dm.Start()
	if err != nil {
		log.Fatalf("Ошибка демонизации: %v", err)
	}

	if !isChild {
		// Parent process: child is daemon. Set up stdin forwarding to socket.
		sockPath := socketPath(dm.daemonProfileName())
		logFile := logFilePath(dm.daemonProfileName())

		sockTimeout := 3 * time.Second
		if *autoSwitch {
			sockTimeout = 15 * time.Second
		}
		sockErr := waitForSocket(sockPath, sockTimeout)
		if sockErr != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Демон запущен, но сокет недоступен: %v\n", sockErr)
		}

		stopCh := make(chan struct{})
		defer close(stopCh)
		go forwardStdinToSocket(sockPath, stopCh)

		fmt.Printf("[*] Подключение к профилю '%s' запущено в фоновом режиме (PID file: %s)\n", daemonProfile, pidFilePath(dm.daemonProfileName()))

		if *logFlag {
			fmt.Println("[*] Вывод лога в реальном времени (Ctrl+C для выхода)")
			go tailLogFile(logFile)
			termCh := make(chan os.Signal, 1)
			signal.Notify(termCh, syscall.SIGINT, syscall.SIGTERM)
			<-termCh
			fmt.Println("\n[*] Остановка...")
		}

		return
	}

	// Child (daemon) process — setup signal handling and proceed
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)

	stopCh := make(chan struct{})
	done := make(chan struct{})
	sw := newSwitchProfileSignal()
	go func() {
		for sig := range sigCh {
			fmt.Printf("\n[*] Получен сигнал: %s\n", sig)
			switch sig {
			case syscall.SIGUSR1:
				if *autoSwitch {
					fmt.Println("[*] Переключение на следующий профиль...")
					sw.trigger()
				} else {
					fmt.Println("[*] Перезагрузка конфигурации...")
				}
			case syscall.SIGUSR2:
				fmt.Println("[*] Переключение уровня логирования...")
			default:
				close(stopCh)
				close(done)
				return
			}
		}
	}()

	_ = done
	cs := newCaptchaSocket(dm.daemonProfileName())
	if err := cs.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[WARNING] Не удалось запустить socket для капчи: %v\n", err)
	}
	defer cs.Stop()
	defer dm.Release()

	// Set active profile for tracking/debug/disconnect.
	// In auto-switch mode, active_profile is always "autoswitch".
	// In tun mode (non-autoswitch), active_profile is set to the specific profile.
	// In socks mode, active_profile is NOT updated, so disconnect/debug targets the
	// tun-mode profile (if any) without being overridden by socks processes.
	if *autoSwitch {
		if err := setActiveProfile("autoswitch"); err != nil {
			fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
		}
		defer func() {
			clearAutoswitchCurrentProfile()
		}()
	} else if *mode == "tun" || *mode == "raw" {
		if err := setActiveProfile(profileName); err != nil {
			fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
		}
	}

	var profiles []string
	if *autoSwitch {
		profiles = listProfileNames()
		if len(profiles) == 0 {
			log.Fatal("Нет доступных профилей")
		}
		if profileName != "" {
			// Move specified profile to front if it exists
			for i, p := range profiles {
				if p == profileName {
					profiles = append([]string{p}, append(profiles[:i], profiles[i+1:]...)...)
					break
				}
			}
		}
	} else {
		if profileName == "" {
			log.Fatal("Профиль не указан")
		}
		profiles = []string{profileName}
	}

	lastAttemptByDeviceID := make(map[string]time.Time)
	const minRetryInterval = 5 * time.Second

	for {
		for idx, currentProfile := range profiles {
			select {
			case <-stopCh:
				return
			default:
			}

			if *autoSwitch && idx > 0 {
				prof, err := loadProfile(currentProfile)
				if err == nil && prof.DeviceID != "" {
					if lastAttempt, exists := lastAttemptByDeviceID[prof.DeviceID]; exists {
						elapsed := time.Since(lastAttempt)
						if elapsed < minRetryInterval {
							waitTime := minRetryInterval - elapsed
							fmt.Printf("[*] Device ID %s использовался недавно, ждем %d секунд...\n",
								prof.DeviceID[:8]+"...", int(waitTime.Seconds()))

							select {
							case <-time.After(waitTime):
							case <-stopCh:
								return
							}
						}
					}
				}
			}

			if idx > 0 {
				fmt.Printf("\n[*] Переключение на профиль '%s'...\n", currentProfile)
			}

			for {
				if *autoSwitch {
					prof, err := loadProfile(currentProfile)
					if err == nil && prof.DeviceID != "" {
						lastAttemptByDeviceID[prof.DeviceID] = time.Now()
					}
				}

				success, wasResume, fatalErr := tryConnectProfile(
					currentProfile,
					*workers, *mtu, *hashes, *dns, *captcha, *timeout,
					*autoSwitch, *mode, *socksPort, *socksUser, *socksPass, *rawPort, *transport,
					sigCh, stopCh, cs.SolveChan(), sw.get(),
					splitCfg,
					false,        // shouldSetActive: handled in connectCmd for non-autoswitch
					!*autoSwitch, // skipActiveProfileClear: clear only in non-autoswitch mode
				)
				if success {
					return
				}

				if fatalErr && *autoSwitch {
					fmt.Println("[ERROR] Критическая ошибка при настройке туннеля — авто-переключение остановлено.")
					fmt.Println("Убедитесь, что проверка 'getcap /path/to/qwdtt' показывает 'cap_net_admin=eip', иначе сделайте:")
					fmt.Println("'sudo setcap cap_net_admin+eip qwdtt' бинарнику")
					fmt.Println("или для NixOS включите 'services.qwdtt.wrappers.enable = true;' (security wrapper с cap_net_admin) с импортом модуля как в README.md")
					cleanupPidFile(daemonProfile)
					removeSplitCfg(daemonProfile)
					_ = teardownWG()
					os.Exit(1)
				}

				select {
				case <-stopCh:
					return
				default:
				}

				if wasResume {
					fmt.Printf("[*] Переподключение к профилю '%s' после resume...\n", currentProfile)
					time.Sleep(2 * time.Second)
					continue
				}

				break
			}

			if idx < len(profiles)-1 {
				fmt.Printf("[!] Профиль '%s' не работает, пробуем следующий...\n", currentProfile)
				time.Sleep(2 * time.Second)
			}
		}

		if !*autoSwitch {
			fmt.Println("[!] Профиль не работает")
			os.Exit(1)
		}

		fmt.Println("[!] Все профили не работают, начинаем сначала...")
		time.Sleep(5 * time.Second)
	}
}

func tryConnectProfile(
	profileName string,
	workers, mtu int,
	hashesOverride, dnsArg, captchaMode string,
	timeoutSec int,
	autoSwitch bool,
	mode string,
	socksPort int,
	socksUser, socksPass string,
	rawPort int,
	transport string,
	sigCh chan os.Signal,
	stopCh chan struct{},
	captchaCh <-chan string,
	switchProfileCh chan struct{},
	splitCfg *splitTunnelConfig,
	shouldSetActive bool,
	skipActiveProfileClear bool,
) (connected bool, wasResume bool, fatal bool) {
	defer removeSplitCfg(profileName)

	prof, err := loadProfile(profileName)
	if err != nil {
		notifyError(profileName, "Ошибка загрузки профиля")
		fmt.Printf("[ERROR] Ошибка загрузки профиля '%s': %v\n", profileName, err)
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		return false, false, false
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
		Workers:     workers,
		CaptchaMode: captchaMode,
		MTU:         mtu,
		DNS:         dnsArg,
		Mode:        mode,
		SocksPort:   socksPort,
		SocksUser:   socksUser,
		SocksPass:   socksPass,
		RawMode:     mode == "raw",
		RawPort:     strconv.Itoa(rawPort),
		Transport:   transport,
	}

	if hashesOverride != "" {
		cfg.Hashes = strings.Split(hashesOverride, ",")
		for i := range cfg.Hashes {
			cfg.Hashes[i] = strings.TrimSpace(cfg.Hashes[i])
		}
	}

	if mode == "socks" {
		// Use a random UDP port for DTLS to allow multiple concurrent socks instances.
		// wireproxy handles WireGuard in userspace, so only the DTLS UDP socket needs to be unique.
		cfg.Listen = "127.0.0.1:0"
	} else if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}

	fmt.Printf("Подключение к профилю '%s'...\n", profileName)
	fmt.Printf("  Peer: %s\n", cfg.PeerAddr)
	fmt.Printf("  Workers: %d\n", cfg.Workers)
	if mode == "socks" {
		fmt.Printf("  Mode: SOCKS5 on port %d\n", socksPort)
	} else if mode == "raw" {
		fmt.Printf("  Mode: raw IP (no WireGuard), server raw port %d\n", rawPort)
	}

	if shouldSetActive {
		if err := setActiveProfile(profileName); err != nil {
			fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
		}
	}

	c, err := core.New(cfg)
	if err != nil {
		notifyError(profileName, "Ошибка запуска")
		fmt.Printf("[ERROR] Ошибка запуска: %v\n", err)
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		return false, false, autoSwitch
	}
	events, err := c.Start()
	if err != nil {
		notifyError(profileName, "Ошибка запуска")
		fmt.Printf("[ERROR] Ошибка запуска: %v\n", err)
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		_ = teardownWG()
		return false, false, autoSwitch
	}

	suspendCh := make(chan bool, 1)
	suspendStopCh := make(chan struct{})
	defer close(suspendStopCh)
	go monitorSuspendResume(suspendCh, suspendStopCh)

	connected = false
	wgConfigured := false
	rawConfigured := false
	wgTested := false
	vkAuthNotified := false
	var wr *core.WireproxyRunner

	var timeout *time.Timer
	if autoSwitch {
		timeout = time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer timeout.Stop()
	}

	teardownMode := func() {
		if wgConfigured {
			teardownWG()
		} else if rawConfigured {
			teardownTunnelRoutes()
		}
		if wr != nil {
			wr.Stop()
		}
	}

	for {
		select {
		case result := <-captchaCh:
			fmt.Println("[*] Получен результат капчи через сокет")
			c.SolveCaptcha(result)
		case <-func() <-chan time.Time {
			if timeout != nil {
				return timeout.C
			}
			return make(<-chan time.Time)
		}():
			fmt.Println("[!] Таймаут подключения")
			notifyError(profileName, "Таймаут подключения")
			c.Stop()
			teardownMode()
			if !skipActiveProfileClear {
				clearActiveProfile()
			}
			return false, false, false
		case <-stopCh:
			c.Stop()
			teardownMode()
			if !skipActiveProfileClear {
				notifyDisconnectedSync(profileName, mode)
				clearActiveProfile()
			}
			return false, false, false
		case <-switchProfileCh:
			fmt.Printf("[*] Принудительное переключение с профиля '%s'...\n", profileName)
			c.Stop()
			teardownMode()
			return false, false, false
		case ev, ok := <-events:
			if !ok {
				teardownMode()
				if !skipActiveProfileClear {
					notifyDisconnectedSync(profileName, mode)
					clearActiveProfile()
				}
				return false, false, false
			}

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
					if autoSwitch {
						if err := setAutoswitchCurrentProfile(profileName); err != nil {
							fmt.Printf("[WARNING] Не удалось сохранить текущий профиль autoswitch: %v\n", err)
						}
					}
				case "disconnected":
					fmt.Println("[*] Отключено")
					teardownMode()
					if !skipActiveProfileClear {
						clearActiveProfile()
					}
					return false, false, false
				}
			case core.EventLog:
				if strings.Contains(ev.Message, "Конфиг получен") {
					fmt.Printf("[OK] %s\n", ev.Message)
				} else if ev.Level == "ERROR" {
					fmt.Printf("[ERROR] %s\n", ev.Message)
				} else if strings.Contains(ev.Message, "FATAL") {
					notifyError(profileName, ev.Message)
					fmt.Printf("[!] %s\n", ev.Message)
					c.Stop()
					teardownMode()
					if !skipActiveProfileClear {
						clearActiveProfile()
					}
					return false, false, false
				}
			case core.EventError:
				fmt.Printf("[ERROR] %s\n", ev.Message)
			case core.EventEvent:
				if mode == "raw" && ev.Name == "raw_conf" && !rawConfigured {
					rawConfigured = true
					fmt.Printf("[*] Raw конфиг получен: %s\n", ev.Data)

					if gateway := defaultGateway(); gateway != nil {
						applyTurnIPsBypass(c.GetTurnIPs(), gateway)
					} else {
						fmt.Printf("[WARNING] Не найден default gateway, маршруты к TURN не настроены\n")
					}

					if splitCfg != nil {
						applyRawSplitTunnel(splitCfg)
					}

					fmt.Println("[*] Проверка работоспособности туннеля...")
					time.Sleep(2 * time.Second)
					if err := testRawConnectivity(); err != nil {
						notifyError(profileName, "Туннель не работает")
						fmt.Printf("[ERROR] Туннель не работает: %v\n", err)
						c.Stop()
						teardownTunnelRoutes()
						if !skipActiveProfileClear {
							clearActiveProfile()
						}
						return false, false, autoSwitch
					}
					fmt.Println("[OK] Туннель работает корректно")
					fmt.Println("[*] Весь трафик теперь идет через VPN")

					if splitCfg != nil {
						_ = writeSplitCfg(profileName, splitCfg.rawBl, splitCfg.rawFile)
					}

					fmt.Printf("[*] Активных воркеров: %d\n", cfg.Workers)
					if autoSwitch {
						if err := setAutoswitchCurrentProfile(profileName); err != nil {
							fmt.Printf("[WARNING] Не удалось сохранить текущий профиль autoswitch: %v\n", err)
						}
					}
					notifyConnected(profileName, mode, int32(cfg.Workers))
					wgTested = true

					if timeout != nil {
						timeout.Stop()
					}
				} else if mode == "raw" && ev.Name == "raw_ready" {
					// Сообщение о готовности raw-туннеля; реальная настройка и
					// проверка сделаны в raw_conf выше.
				} else if ev.Name == "wg_config" && !wgConfigured {
					wgConfigured = true
					fmt.Printf("[*] WireGuard конфиг получен (%d байт)\n", len(ev.Data))

					if mode == "socks" {
						var bypassDomains []string
						var bypassIPs []string
						if splitCfg != nil && len(splitCfg.domains) > 0 {
							bypassDomains = splitCfg.domains
							bypassIPs, _ = resolveDomainIPs(splitCfg.domains)
						}
						wr = core.NewWireproxyRunner(socksPort, socksUser, socksPass, bypassDomains, bypassIPs)
						if err := wr.Start(context.Background(), ev.Data); err != nil {
							notifyError(profileName, "Не удалось запустить SOCKS5 сервер")
							fmt.Printf("[ERROR] Не удалось запустить SOCKS5 сервер: %v\n", err)
							c.Stop()
							if !skipActiveProfileClear {
								clearActiveProfile()
							}
							return false, false, false
						}
						fmt.Printf("[OK] SOCKS5 сервер запущен на порту %d\n", socksPort)
						if splitCfg != nil && len(splitCfg.domains) > 0 {
							fmt.Printf("[*] Split tunnel (blacklist): %d доменов, %d IP напрямую\n",
								len(splitCfg.domains), len(bypassIPs))
						}
						if splitCfg != nil {
							_ = writeSplitCfg(profileName, splitCfg.rawBl, splitCfg.rawFile)
						}
						fmt.Printf("[*] Активных воркеров: %d\n", cfg.Workers)
						if autoSwitch {
							if err := setAutoswitchCurrentProfile(profileName); err != nil {
								fmt.Printf("[WARNING] Не удалось сохранить текущий профиль autoswitch: %v\n", err)
							}
						}
						notifyConnected(profileName, mode, int32(cfg.Workers))
						wgTested = true

						if timeout != nil {
							timeout.Stop()
						}
					} else {
						turnIPs := c.GetTurnIPs()
						fmt.Printf("[*] Настройка интерфейса wg-qwdtt...\n")
						if err := applyWGConfig(ev.Data, turnIPs, splitCfg); err != nil {
							notifyError(profileName, "Ошибка настройки WireGuard")
							fmt.Printf("[ERROR] Ошибка настройки WireGuard: %v\n", err)
							fmt.Println("Убедитесь, что 'getcap /run/wrappers/bin/qwdtt' показывает 'cap_net_admin=eip', иначе сделайте:")
							fmt.Println("'sudo setcap cap_net_admin+eip qwdtt' бинарнику")
							fmt.Println("или для NixOS включите 'services.qwdtt.wrappers.enable = true;' (security wrapper с cap_net_admin) с импортом модуля как в README.md")
							c.Stop()
							teardownWG()
							if !skipActiveProfileClear {
								clearActiveProfile()
							}
							return false, false, true
						}
						fmt.Println("[OK] WireGuard интерфейс настроен и активен")

						fmt.Println("[*] Проверка работоспособности туннеля...")
						time.Sleep(2 * time.Second)
					if err := testWGConnectivity(); err != nil {
						notifyError(profileName, "Туннель не работает")
						fmt.Printf("[ERROR] Туннель не работает: %v\n", err)
						c.Stop()
						teardownWG()
						if !skipActiveProfileClear {
							clearActiveProfile()
						}
						return false, false, autoSwitch
					}
						fmt.Println("[OK] Туннель работает корректно")
						fmt.Println("[*] Весь трафик теперь идет через VPN")

						if splitCfg != nil {
							_ = writeSplitCfg(profileName, splitCfg.rawBl, splitCfg.rawFile)
						}

						fmt.Printf("[*] Активных воркеров: %d\n", cfg.Workers)
						if autoSwitch {
							if err := setAutoswitchCurrentProfile(profileName); err != nil {
								fmt.Printf("[WARNING] Не удалось сохранить текущий профиль autoswitch: %v\n", err)
							}
						}
						notifyConnected(profileName, mode, int32(cfg.Workers))
						wgTested = true

						if timeout != nil {
							timeout.Stop()
						}
					}

				} else if ev.Name == "workers_completed" {
					fmt.Println("[*] All workers completed")
					teardownMode()
					if !skipActiveProfileClear {
						notifyDisconnectedSync(profileName, mode)
						clearActiveProfile()
					}
					return false, false, false
				} else if ev.Name == "captcha_required" {
					parts := strings.Split(ev.Data, "|")
					if len(parts) >= 1 {
						fmt.Printf("[!] Captcha required (mode: %s)\n", parts[0])
					}
				}
			case core.EventStats:
				if connected && wgTested && ev.RxBytes > 0 {
					fmt.Printf("\r[STATS] RX: %s | TX: %s | Workers: %d   ",
						formatBytes(ev.RxBytes),
						formatBytes(ev.TxBytes),
						ev.Workers)
					notifyWorkers(profileName, ev.Workers)
				}
			}
			if !vkAuthNotified && c.IsVKAuthPassed() {
				vkAuthNotified = true
				notifyVKAuth(profileName)
			}
		case <-suspendCh:
			fmt.Println("\n[*] Обнаружен resume, переподключение...")
			c.Stop()
			teardownMode()
			if !skipActiveProfileClear {
				notifyDisconnectedSync(profileName, mode)
				clearActiveProfile()
			}
			return false, true, false
		}
	}
}

// tailLogFile follows a log file and prints new lines to stdout.
func tailLogFile(path string) {
	for {
		file, err := os.Open(path)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		file.Seek(0, io.SeekEnd)
		reader := bufio.NewReader(file)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				file.Close()
				break
			}
			fmt.Print(line)
		}
	}
}
