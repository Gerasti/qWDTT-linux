package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"qwdtt/internal/core"
)

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

	var profiles []string
	if *autoSwitch {
		profiles = listProfileNames()
		if len(profiles) == 0 {
			log.Fatal("Нет доступных профилей")
		}
		if profileName != "" {
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	stopCh := make(chan struct{})
	go func() {
		<-sigCh
		fmt.Println("\nОтключение...")
		close(stopCh)
	}()

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

				success, wasResume := tryConnectProfile(currentProfile, *workers, *mtu, *hashes, *dns, *captcha, *timeout, *autoSwitch, *mode, *socksPort, sigCh, stopCh)
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

func tryConnectProfile(profileName string, workers, mtu int, hashesOverride, dnsArg, captchaMode string, timeoutSec int, autoSwitch bool, mode string, socksPort int, sigCh chan os.Signal, stopCh chan struct{}) (bool, bool) {
	prof, err := loadProfile(profileName)
	if err != nil {
		fmt.Printf("[ERROR] Ошибка загрузки профиля '%s': %v\n", profileName, err)
		clearActiveProfile()
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
		cfg.Listen = "127.0.0.1:9001"
	} else if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}

	fmt.Printf("Подключение к профилю '%s'...\n", profileName)
	fmt.Printf("  Peer: %s\n", cfg.PeerAddr)
	fmt.Printf("  Workers: %d\n", cfg.Workers)
	if mode == "socks" {
		fmt.Printf("  Mode: SOCKS5 on port %d\n", socksPort)
	}

	if err := setActiveProfile(profileName); err != nil {
		fmt.Printf("[WARNING] Не удалось сохранить активный профиль: %v\n", err)
	}

	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		fmt.Printf("[ERROR] Ошибка запуска: %v\n", err)
		clearActiveProfile()
		return false, false
	}

	suspendCh := make(chan bool, 1)
	suspendStopCh := make(chan struct{})
	defer close(suspendStopCh)
	go monitorSuspendResume(suspendCh, suspendStopCh)

	go func() {
		<-sigCh
		fmt.Println("\nОтключение...")
		c.Stop()
	}()

	connected := false
	wgConfigured := false
	wgTested := false
	var coreInstance *core.Core
	var wr *core.WireproxyRunner
	coreInstance = c

	// stdin reader for CAPTCHA_RESULT protocol
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "CAPTCHA_RESULT|") {
				result := strings.TrimPrefix(line, "CAPTCHA_RESULT|")
				coreInstance.SolveCaptcha(result)
			}
		}
	}()

	var timeout *time.Timer
	if autoSwitch {
		timeout = time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer timeout.Stop()
	}

	for {
		select {
		case <-func() <-chan time.Time {
			if timeout != nil {
				return timeout.C
			}
			return make(<-chan time.Time)
		}():
			fmt.Println("[!] Таймаут подключения")
			c.Stop()
			if wgConfigured {
				teardownWG()
			}
			if wr != nil {
				wr.Stop()
			}
			clearActiveProfile()
			return false, false
		case <-stopCh:
			c.Stop()
			if wgConfigured {
				fmt.Println("\n[*] Удаление WireGuard интерфейса...")
				teardownWG()
			}
			if wr != nil {
				wr.Stop()
			}
			clearActiveProfile()
			return false, false
		case <-suspendCh:
			fmt.Println("\n[*] Обнаружен resume, переподключение...")
			c.Stop()
			if wgConfigured {
				teardownWG()
			}
			if wr != nil {
				wr.Stop()
			}
			clearActiveProfile()
			return false, true
		case ev, ok := <-events:
			if !ok {
				if wgConfigured {
					teardownWG()
				}
				if wr != nil {
					wr.Stop()
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
					clearActiveProfile()
					return false, false
				}
			case core.EventLog:
				if strings.Contains(ev.Message, "Конфиг получен") {
					fmt.Printf("[OK] %s\n", ev.Message)
				} else if ev.Level == "ERROR" {
					fmt.Printf("[ERROR] %s\n", ev.Message)
				} else if strings.Contains(ev.Message, "FATAL") {
					fmt.Printf("[!] %s\n", ev.Message)
					c.Stop()
					if wgConfigured {
						teardownWG()
					}
					if wr != nil {
						wr.Stop()
					}
					clearActiveProfile()
					return false, false
				}
			case core.EventError:
				fmt.Printf("[ERROR] %s\n", ev.Message)
			case core.EventEvent:
				if ev.Name == "wg_config" && !wgConfigured {
					wgConfigured = true
					fmt.Printf("[*] WireGuard конфиг получен (%d байт)\n", len(ev.Data))

					if mode == "socks" {
						// SOCKS5 mode: use wireproxy for userspace WireGuard + SOCKS5
						wr = core.NewWireproxyRunner(socksPort)
						if err := wr.Start(context.Background(), ev.Data); err != nil {
							fmt.Printf("[ERROR] Не удалось запустить SOCKS5 сервер: %v\n", err)
							c.Stop()
							clearActiveProfile()
							return false, false
						}
						fmt.Printf("[OK] SOCKS5 сервер запущен на порту %d\n", socksPort)
						wgTested = true // Mark as tested so we can exit the connection loop

						if timeout != nil {
							timeout.Stop()
						}
					} else {
						// Kernel mode: configure WG interface as before
						turnIPs := coreInstance.GetTurnIPs()
						fmt.Printf("[*] Настройка интерфейса wg-qwdtt...\n")
						if err := applyWGConfig(ev.Data, turnIPs); err != nil {
							fmt.Printf("[ERROR] Ошибка настройки WireGuard: %v\n", err)
							fmt.Println("  Убедитесь, что:")
							fmt.Println("  1. Команды ip и wg доступны")
							fmt.Println("  2. В /etc/sudoers добавлено: your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg")
							c.Stop()
							clearActiveProfile()
							return false, false
						}
						fmt.Println("[OK] WireGuard интерфейс настроен и активен")

						fmt.Println("[*] Проверка работоспособности туннеля...")
						time.Sleep(2 * time.Second)
						if err := testWGConnectivity(); err != nil {
							fmt.Printf("[ERROR] Туннель не работает: %v\n", err)
							c.Stop()
							teardownWG()
							clearActiveProfile()
							return false, false
						}
						fmt.Println("[OK] Туннель работает корректно")
						fmt.Println("[*] Весь трафик теперь идет через VPN")
						wgTested = true

						if timeout != nil {
							timeout.Stop()
						}
					}

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
				}
			}
		}

		if wgTested && connected {
			var healthCheckTicker *time.Ticker
			if autoSwitch {
				healthCheckTicker = time.NewTicker(30 * time.Second)
				defer healthCheckTicker.Stop()
			}

			for {
				select {
				case <-sigCh:
					c.Stop()
					if wgConfigured {
						fmt.Println("\n[*] Удаление WireGuard интерфейса...")
						if err := teardownWG(); err != nil {
							fmt.Printf("[!] Ошибка при удалении интерфейса: %v\n", err)
						}
					}
					if wr != nil {
						wr.Stop()
					}
					fmt.Println("[*] Завершено")
					clearActiveProfile()
					return true, false
				case <-stopCh:
					c.Stop()
					if wgConfigured {
						fmt.Println("\n[*] Удаление WireGuard интерфейса...")
						if err := teardownWG(); err != nil {
							fmt.Printf("[!] Ошибка при удалении интерфейса: %v\n", err)
						}
					}
					if wr != nil {
						wr.Stop()
					}
					clearActiveProfile()
					return false, false
				case <-suspendCh:
					fmt.Println("\n[*] Обнаружен resume, переподключение...")
					c.Stop()
					if wgConfigured {
						teardownWG()
					}
					if wr != nil {
						wr.Stop()
					}
					return false, true
				case <-func() <-chan time.Time {
					if healthCheckTicker != nil {
						return healthCheckTicker.C
					}
					return make(<-chan time.Time)
				}():
					if mode == "kernel" {
						if err := testWGConnectivity(); err != nil {
							fmt.Printf("\n[ERROR] Туннель перестал работать: %v\n", err)
							c.Stop()
							if wgConfigured {
								teardownWG()
							}
							return false, false
						}
					}
				case ev, ok := <-events:
					if !ok {
						if wgConfigured {
							teardownWG()
						}
						if wr != nil {
							wr.Stop()
						}
						return false, false
					}

					switch ev.Type {
					case core.EventState:
						if ev.Status == "disconnected" {
							fmt.Println("[*] Отключено")
							if wgConfigured {
								teardownWG()
							}
							if wr != nil {
								wr.Stop()
							}
							return false, false
						}
					case core.EventError:
						fmt.Printf("[ERROR] %s\n", ev.Message)
					case core.EventStats:
						if ev.RxBytes > 0 {
							fmt.Printf("\r[STATS] RX: %s | TX: %s | Workers: %d   ",
								formatBytes(ev.RxBytes),
								formatBytes(ev.TxBytes),
								ev.Workers)
						}
					}
				}
			}
		}
	}
}