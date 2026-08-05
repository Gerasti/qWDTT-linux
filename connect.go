package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"qwdtt/internal/core"
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

// isKernelInterfaceActive checks if the WireGuard kernel interface (wg-qwdtt) is up.
func isKernelInterfaceActive() bool {
	cmd := exec.Command("ip", "link", "show", wgIface)
	return cmd.Run() == nil
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
	mode := fs.String("mode", "kernel", "Connection mode (kernel|socks)")
	socksPort := fs.Int("socks-port", 9050, "SOCKS5 port (only with -mode socks)")

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

	// --- Pre-connection conflict checks (parent process) ---
	// These checks run before daemonization so we can use notify/fmt freely.
	if *autoSwitch {
		// Check if autoswitch daemon is already running
		if isDaemonRunning("autoswitch") {
			msg := "Авто-переключение уже запущено (PID file: qwdtt-autoswitch.pid). Используйте qwdtt discon для остановки."
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
		if *mode == "kernel" {
			// Kernel mode: only one connection allowed (wg-qwdtt interface)
			if isActiveProfile("autoswitch") {
				msg := "Уже запущено авто-переключение в режиме kernel. Используйте qwdtt discon для остановки."
				fmt.Fprintln(os.Stderr, "[ERROR] "+msg)
				notifyError(profileName, msg)
				os.Exit(1)
			}
			if otherProfile := getActiveProfile(); otherProfile != "" && otherProfile != profileName {
				msg := fmt.Sprintf("Уже активен профиль '%s' в режиме kernel. Используйте qwdtt discon для остановки.", otherProfile)
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
		} else if *mode == "socks" {
			// SOCKS mode: multiple connections allowed with different ports.
			// Do not block on kernel interface (socks mode does not conflict with it),
			// as each socks instance uses a unique DTLS port (Listen=127.0.0.1:0, different from kernel's 9000).
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

		if err := waitForSocket(sockPath, 3*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "[WARNING] Демон запущен, но сокет недоступен: %v\n", err)
			fmt.Fprintf(os.Stderr, "[*] Подключение к профилю '%s' запущено в фоновом режиме\n", daemonProfile)
			return
		}

		stopCh := make(chan struct{})
		defer close(stopCh)
		go forwardStdinToSocket(sockPath, stopCh)

		fmt.Printf("[*] Подключение к профилю '%s' запущено в фоновом режиме (PID file: %s)\n", daemonProfile, pidFilePath(dm.daemonProfileName()))
		fmt.Println("[*] Для ввода CAPTCHA_RESULT| ответа капчи в терминал")
		return
	}

	// Child (daemon) process — setup signal handling and proceed
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)

	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for sig := range sigCh {
			fmt.Printf("\n[*] Получен сигнал: %s\n", sig)
			switch sig {
			case syscall.SIGUSR1:
				fmt.Println("[*] Перезагрузка конфигурации...")
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

	// Set active profile for tracking/debug/disconnect
	if *autoSwitch {
		// In auto-switch mode, active_profile is always "autoswitch"
		if err := setActiveProfile("autoswitch"); err != nil {
			fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
		}
	} else {
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

				success, wasResume := tryConnectProfile(
					currentProfile,
					*workers, *mtu, *hashes, *dns, *captcha, *timeout,
					*autoSwitch, *mode, *socksPort,
					sigCh, stopCh, cs.SolveChan(),
					false, // shouldSetActive: handled in connectCmd for non-autoswitch
					!*autoSwitch, // skipActiveProfileClear: clear only in non-autoswitch mode
				)
				if success {
					return
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
	sigCh chan os.Signal,
	stopCh chan struct{},
	captchaCh <-chan string,
	shouldSetActive bool,
	skipActiveProfileClear bool,
) (connected bool, wasResume bool) {
	prof, err := loadProfile(profileName)
	if err != nil {
		notifyError(profileName, "Ошибка загрузки профиля")
		fmt.Printf("[ERROR] Ошибка загрузки профиля '%s': %v\n", profileName, err)
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		return false, false
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
	}

	if shouldSetActive {
		if err := setActiveProfile(profileName); err != nil {
			fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
		}
	}

	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		notifyError(profileName, "Ошибка запуска")
		fmt.Printf("[ERROR] Ошибка запуска: %v\n", err)
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		return false, false
	}

	suspendCh := make(chan bool, 1)
	suspendStopCh := make(chan struct{})
	defer close(suspendStopCh)
	go monitorSuspendResume(suspendCh, suspendStopCh)

	connected = false
	wgConfigured := false
	wgTested := false
	var wr *core.WireproxyRunner

	var timeout *time.Timer
	if autoSwitch {
		timeout = time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer timeout.Stop()
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
		if wgConfigured {
			teardownWG()
		}
		if wr != nil {
			wr.Stop()
		}
		if !skipActiveProfileClear {
			clearActiveProfile()
		}
		return false, false
	case <-stopCh:
		if !skipActiveProfileClear {
			notifyDisconnectedSync(profileName)
			clearActiveProfile()
		}
		return false, false
	case ev, ok := <-events:
			if !ok {
				if wgConfigured {
					teardownWG()
				}
				if wr != nil {
					wr.Stop()
				}
				if !skipActiveProfileClear {
					notifyDisconnectedSync(profileName)
					clearActiveProfile()
				}
				return false, false
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
				case "disconnected":
					fmt.Println("[*] Отключено")
					if wgConfigured {
						teardownWG()
					}
					if wr != nil {
						wr.Stop()
					}
					if !skipActiveProfileClear {
						clearActiveProfile()
					}
					return false, false
				}
			case core.EventLog:
				if strings.Contains(ev.Message, "Конфиг получен") {
					fmt.Printf("[OK] %s\n", ev.Message)
				} else if strings.Contains(ev.Message, "[VK Auth] Success") {
					notifyVKAuth(profileName)
				} else if ev.Level == "ERROR" {
					fmt.Printf("[ERROR] %s\n", ev.Message)
				} else if strings.Contains(ev.Message, "FATAL") {
					notifyError(profileName, ev.Message)
					fmt.Printf("[!] %s\n", ev.Message)
					c.Stop()
					if wgConfigured {
						teardownWG()
					}
					if wr != nil {
						wr.Stop()
					}
					if !skipActiveProfileClear {
						clearActiveProfile()
					}
					return false, false
				}
			case core.EventError:
				fmt.Printf("[ERROR] %s\n", ev.Message)
			case core.EventEvent:
				if ev.Name == "wg_config" && !wgConfigured {
					wgConfigured = true
					fmt.Printf("[*] WireGuard конфиг получен (%d байт)\n", len(ev.Data))

					if mode == "socks" {
						wr = core.NewWireproxyRunner(socksPort)
						if err := wr.Start(context.Background(), ev.Data); err != nil {
							notifyError(profileName, "Не удалось запустить SOCKS5 сервер")
							fmt.Printf("[ERROR] Не удалось запустить SOCKS5 сервер: %v\n", err)
							c.Stop()
							if !skipActiveProfileClear {
								clearActiveProfile()
							}
							return false, false
						}
						fmt.Printf("[OK] SOCKS5 сервер запущен на порту %d\n", socksPort)
						fmt.Printf("[*] Активных воркеров: %d\n", cfg.Workers)
						notifyConnected(profileName, int32(cfg.Workers))
						wgTested = true

						if timeout != nil {
							timeout.Stop()
						}
					} else {
						turnIPs := c.GetTurnIPs()
						fmt.Printf("[*] Настройка интерфейса wg-qwdtt...\n")
						if err := applyWGConfig(ev.Data, turnIPs); err != nil {
							notifyError(profileName, "Ошибка настройки WireGuard")
							fmt.Printf("[ERROR] Ошибка настройки WireGuard: %v\n", err)
							fmt.Println("  Убедитесь, что:")
							fmt.Println("  1. Команды ip и wg доступны")
							fmt.Println("  2. В /etc/sudoers добавлено: your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg")
							c.Stop()
							if !skipActiveProfileClear {
								clearActiveProfile()
							}
							return false, false
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
							return false, false
						}
						fmt.Println("[OK] Туннель работает корректно")
						fmt.Println("[*] Весь трафик теперь идет через VPN")
						fmt.Printf("[*] Активных воркеров: %d\n", cfg.Workers)
						notifyConnected(profileName, int32(cfg.Workers))
						wgTested = true

						if timeout != nil {
							timeout.Stop()
						}
					}

				} else if ev.Name == "workers_completed" {
					fmt.Println("[*] All workers completed")
					if wgConfigured {
						teardownWG()
					}
					if wr != nil {
						wr.Stop()
					}
					if !skipActiveProfileClear {
						notifyDisconnectedSync(profileName)
						clearActiveProfile()
					}
					return false, false
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
				}			}
		case <-suspendCh:
			fmt.Println("\n[*] Обнаружен resume, переподключение...")
			c.Stop()
			if wgConfigured {
				teardownWG()
			}
			if wr != nil {
				wr.Stop()
			}
			if !skipActiveProfileClear {
				notifyDisconnectedSync(profileName)
				clearActiveProfile()
			}
			return false, true
		}
	}
}

