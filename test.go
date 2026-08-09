package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"qwdtt/internal/core"
)

const defaultTestTimeoutSec = 10

// TestResult holds the outcome of testing a single profile.
type TestResult struct {
	Profile       string
	VKAuth        string // "✓" / "✗"
	Workers       int32
	Connect       string // "✓" / "✗"
	InternetCheck string // "✓" / "✗" / "n/a"
	Error         string
}

// getProfilePriority returns the priority of a profile (0 if it can't be loaded).
func getProfilePriority(name string) int {
	prof, err := loadProfile(name)
	if err != nil {
		return 0
	}
	return prof.Priority
}

func testCmd() {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	timeoutSec := fs.Int("timeout", defaultTestTimeoutSec, "Connection timeout in seconds")
	ro := fs.Bool("ro", false, "Test only read-only profiles")
	en := fs.Bool("en", false, "Test only enabled profiles (alias for -enabled)")
	enabled := fs.Bool("enabled", false, "Test only enabled profiles")
	dis := fs.Bool("dis", false, "Test only disabled profiles (alias for -disabled)")
	disabled := fs.Bool("disabled", false, "Test only disabled profiles")
	mode := fs.String("mode", "tun", "Connection mode (tun|socks)")
	socksPort := fs.Int("socks-port", defaultSocksPort, "SOCKS5 port (only with -mode socks)")

	var profileName string
	if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
		profileName = os.Args[2]
		fs.Parse(os.Args[3:])
	} else {
		fs.Parse(os.Args[2:])
		if profileName == "" && fs.NArg() > 0 {
			profileName = fs.Arg(0)
		}
	}

	if *mode != "tun" && *mode != "socks" {
		fmt.Fprintf(os.Stderr, "[ERROR] Недопустимый режим: %s (доступные: tun, socks)\n", *mode)
		os.Exit(1)
	}

	testEnabled := *en || *enabled
	testDisabled := *dis || *disabled

	if testEnabled && testDisabled {
		fmt.Fprintln(os.Stderr, "[ERROR] Нельзя одновременно использовать -enabled и -disabled")
		os.Exit(1)
	}

	var targetProfiles []string
	if profileName != "" {
		if strings.HasPrefix(profileName, "wdtt://") {
			link, err := parseWdttURL(profileName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ERROR] Неверная ссылка: %v\n", err)
				os.Exit(1)
			}
			testProfileFromLink(*link, time.Duration(*timeoutSec)*time.Second, *mode, *socksPort, profileName)
			return
		}
		if *ro && !strings.HasPrefix(profileName, "ro-") {
			fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' не является read-only\n", profileName)
			os.Exit(1)
		}
		if testEnabled && !isProfileEnabled(profileName) {
			fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' отключён\n", profileName)
			os.Exit(1)
		}
		if testDisabled && isProfileEnabled(profileName) {
			fmt.Fprintf(os.Stderr, "[ERROR] Профиль '%s' включён\n", profileName)
			os.Exit(1)
		}
		targetProfiles = []string{profileName}
	} else {
		if *ro {
			targetProfiles = listReadOnlyProfileNames()
		} else {
			targetProfiles = listAllProfileNames()
		}

		if testEnabled {
			var filtered []string
			for _, name := range targetProfiles {
				if isProfileEnabled(name) {
					filtered = append(filtered, name)
				}
			}
			targetProfiles = filtered
		}
		if testDisabled {
			var filtered []string
			for _, name := range targetProfiles {
				if !isProfileEnabled(name) {
					filtered = append(filtered, name)
				}
			}
			targetProfiles = filtered
		}
	}

	if len(targetProfiles) == 0 {
		labels := []string{}
		if *ro {
			labels = append(labels, "read-only")
		}
		if testEnabled {
			labels = append(labels, "enabled")
		}
		if testDisabled {
			labels = append(labels, "disabled")
		}
		if len(labels) > 0 {
			fmt.Printf("Нет %s профилей для тестирования\n", strings.Join(labels, ", "))
		} else {
			fmt.Println("Нет профилей для тестирования")
		}
		return
	}

	modeStr := *mode
	if *mode == "socks" {
		modeStr = fmt.Sprintf("socks (port %d)", *socksPort)
	}
	fmt.Printf("Тестирование %d профилей (mode: %s, timeout: %ds)\n\n", len(targetProfiles), modeStr, *timeoutSec)

 	var passCount, failCount int
	// Sort profiles by priority (highest first), then by name
	sort.Slice(targetProfiles, func(i, j int) bool {
		pi := getProfilePriority(targetProfiles[i])
		pj := getProfilePriority(targetProfiles[j])
		if pi != pj {
			return pi > pj
		}
		return targetProfiles[i] < targetProfiles[j]
	})
	for idx, name := range targetProfiles {
		result := testProfile(name, time.Duration(*timeoutSec)*time.Second, *mode, *socksPort)
		if result.VKAuth == "✓" && result.Connect == "✓" && result.Workers > 0 && result.InternetCheck == "✓" {
			passCount++
		} else {
			failCount++
		}
		if idx < len(targetProfiles)-1 {
			time.Sleep(5 * time.Second)
		}
	}

	fmt.Printf("=== Summary: %d passed, %d failed ===\n", passCount, failCount)
}

func testProfile(name string, timeout time.Duration, mode string, socksPort int) TestResult {
	// Suppress internal log output — only show test stage results
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origLogOutput)

	result := TestResult{
		Profile:       name,
		VKAuth:        "✗",
		Connect:       "✗",
		InternetCheck: "n/a",
		Workers:       0,
	}

	fmt.Printf("=== Testing: %s ===\n", name)

	if isDaemonRunning(name) {
		result.Error = "daemon already running"
		fmt.Printf("  [✗] VKAuth (daemon уже запущен)\n")
		fmt.Printf("  [✗] Workers: 0\n")
		fmt.Printf("  [✗] Connect\n")
		fmt.Printf("  [✗] InternetCheck (n/a)\n")
		fmt.Printf("  → FAIL\n\n")
		return result
	}

	prof, err := loadProfile(name)
	if err != nil {
		result.Error = err.Error()
		fmt.Printf("  [✗] VKAuth (ошибка загрузки профиля: %v)\n", err)
		fmt.Printf("  [✗] Workers: 0\n")
		fmt.Printf("  [✗] Connect\n")
		fmt.Printf("  [✗] InternetCheck (n/a)\n")
		fmt.Printf("  → FAIL\n\n")
		return result
	}

	deviceID := prof.DeviceID
	if deviceID == "" {
		deviceID = getOrCreateDeviceID()
	}

	cfg := core.Config{
		PeerAddr:    prof.PeerAddr,
		Password:    prof.Password,
		Hashes:      prof.Hashes,
		Listen:      "127.0.0.1:9000",
		TurnHost:    prof.TurnHost,
		TurnPort:    prof.TurnPort,
		DeviceID:    deviceID,
		Workers:     9,
		CaptchaMode: "auto",
		MTU:         1280,
		DNS:         "yandex",
		Mode:        mode,
		SocksPort:   socksPort,
	}
	if mode == "socks" {
		cfg.Listen = "127.0.0.1:0"
	} else if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}

	cs := newCaptchaSocket(name)
	_ = cs.Start()
	defer cs.Stop()

	stopStdinCh := make(chan struct{})
	defer close(stopStdinCh)
	go forwardStdinToSocket(socketPath(name), stopStdinCh)

	c := core.New(cfg)
	defer c.Stop()

	events, err := c.Start()
	if err != nil {
		result.Error = err.Error()
		fmt.Printf("  [✗] VKAuth (ошибка запуска: %v)\n", err)
		fmt.Printf("  [✗] Workers: 0\n")
		fmt.Printf("  [✗] Connect\n")
		fmt.Printf("  [✗] InternetCheck (n/a)\n")
		fmt.Printf("  → FAIL\n\n")
		return result
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	vkAuthPassed := false
	connectPassed := false
	workersPassed := false
	wgApplied := false
	var wr *core.WireproxyRunner

	done := false
	for !done {
		select {
		case <-timer.C:
			done = true
		case result := <-cs.SolveChan():
			fmt.Println("[*] Получен результат капчи через сокет")
			c.SolveCaptcha(result)
		case ev, ok := <-events:
			if !ok {
				done = true
				continue
			}
			switch ev.Type {
			case core.EventState:
				if ev.Status == "connecting" {
					fmt.Printf("  [*] Подключение...\n")
				}
			case core.EventLog:
				if strings.Contains(ev.Message, "[VK Auth] Success") && !vkAuthPassed {
					vkAuthPassed = true
					result.VKAuth = "✓"
					fmt.Printf("  [✓] VKAuth\n")
				}
			case core.EventEvent:
				if ev.Name == "wg_config" && !connectPassed {
					wgApplied = true
					connectErr := error(nil)
					if cfg.Mode == "socks" {
						wr = core.NewWireproxyRunner(cfg.SocksPort)
						connectErr = wr.Start(context.Background(), ev.Data)
					} else {
						turnIPs := c.GetTurnIPs()
						connectErr = applyWGConfig(ev.Data, turnIPs, nil)
					}
					if connectErr != nil {
						fmt.Printf("  [✗] Connect (%v)\n", connectErr)
						done = true
						continue
					}
					connectPassed = true
					result.Connect = "✓"
					fmt.Printf("  [✓] Connect (%s)\n", cfg.Mode)
					if cfg.Mode == "tun" {
						result.InternetCheck = testInternetCheck("tun", 0)
					} else {
						result.InternetCheck = testInternetCheck("socks", cfg.SocksPort)
					}
				}
				if ev.Name == "captcha_required" {
					parts := strings.Split(ev.Data, "|")
					if len(parts) >= 1 {
						fmt.Printf("  [!] Капча требуется (режим: %s)\n", parts[0])
					}
				}
				if ev.Name == "workers_completed" {
					done = true
				}
			case core.EventStats:
				if ev.Workers > 0 && !workersPassed {
					workersPassed = true
					result.Workers = ev.Workers
					fmt.Printf("  [✓] Workers: %d\n", ev.Workers)
				}
			case core.EventError:
				fmt.Printf("  [✗] Ошибка ядра: %s\n", ev.Message)
				done = true
			}
		}
	}

	c.Stop()
	// Wait for Core's goroutine to fully exit and release the UDP socket.
	// The events channel is closed once the goroutine exits.
	drained := make(chan struct{})
	go func() {
		for range events {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
	}

	if wgApplied {
		if cfg.Mode == "socks" && wr != nil {
			_ = wr.Stop()
		} else {
			_ = teardownWG()
		}
		// Brief delay for kernel to release the wg-qwdtt interface
		time.Sleep(500 * time.Millisecond)
	}

	if !vkAuthPassed {
		fmt.Printf("  [✗] VKAuth\n")
	}
	if !workersPassed {
		fmt.Printf("  [✗] Workers: 0\n")
	}
	if result.InternetCheck == "n/a" && connectPassed {
		result.InternetCheck = "n/a"
	}

	allPassed := vkAuthPassed && connectPassed && workersPassed && result.InternetCheck == "✓"
	if allPassed {
		fmt.Printf("  → PASS\n\n")
	} else {
		fmt.Printf("  → FAIL\n\n")
	}

	return result
}

// testInternetCheck pings 8.8.8.8 and 1.1.1.1 through the tunnel/proxy.
// In tun mode, pings directly through the wg-qwdtt interface.
// In socks mode, uses curl via the SOCKS5 proxy.
func testProfileFromLink(link WdttLink, timeout time.Duration, mode string, socksPort int, linkStr string) TestResult {
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origLogOutput)

	result := TestResult{
		Profile:       linkStr,
		VKAuth:        "✗",
		Connect:       "✗",
		InternetCheck: "n/a",
		Workers:       0,
	}

	fmt.Printf("=== Testing: %s ===\n", linkStr)

	deviceID := getOrCreateDeviceID()

	cfg := core.Config{
		PeerAddr:    fmt.Sprintf("%s:%s", link.IP, link.DTLSPort),
		Password:    link.Password,
		Hashes:      link.Hashes,
		Listen:      "127.0.0.1:9000",
		TurnHost:    "",
		TurnPort:    "",
		DeviceID:    deviceID,
		Workers:     9,
		CaptchaMode: "auto",
		MTU:         1280,
		DNS:         "yandex",
		Mode:        mode,
		SocksPort:   socksPort,
	}
	if mode == "socks" {
		cfg.Listen = "127.0.0.1:0"
	} else if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}

	c := core.New(cfg)
	defer c.Stop()

	events, err := c.Start()
	if err != nil {
		result.Error = err.Error()
		fmt.Printf("  [✗] VKAuth (ошибка запуска: %v)\n", err)
		fmt.Printf("  [✗] Workers: 0\n")
		fmt.Printf("  [✗] Connect\n")
		fmt.Printf("  [✗] InternetCheck (n/a)\n")
		fmt.Printf("  → FAIL\n\n")
		return result
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	vkAuthPassed := false
	connectPassed := false
	workersPassed := false
	wgApplied := false
	var wr *core.WireproxyRunner

	done := false
	for !done {
		select {
		case <-timer.C:
			done = true
		case ev, ok := <-events:
			if !ok {
				done = true
				continue
			}
			switch ev.Type {
			case core.EventState:
				if ev.Status == "connecting" {
					fmt.Printf("  [*] Подключение...\n")
				}
			case core.EventLog:
				if strings.Contains(ev.Message, "[VK Auth] Success") && !vkAuthPassed {
					vkAuthPassed = true
					result.VKAuth = "✓"
					fmt.Printf("  [✓] VKAuth\n")
				}
			case core.EventEvent:
				if ev.Name == "wg_config" && !connectPassed {
					wgApplied = true
					connectErr := error(nil)
					if cfg.Mode == "socks" {
						wr = core.NewWireproxyRunner(cfg.SocksPort)
						connectErr = wr.Start(context.Background(), ev.Data)
					} else {
						turnIPs := c.GetTurnIPs()
						connectErr = applyWGConfig(ev.Data, turnIPs, nil)
					}
					if connectErr != nil {
						fmt.Printf("  [✗] Connect (%v)\n", connectErr)
						done = true
						continue
					}
					connectPassed = true
					result.Connect = "✓"
					fmt.Printf("  [✓] Connect (%s)\n", cfg.Mode)
					if cfg.Mode == "tun" {
						result.InternetCheck = testInternetCheck("tun", 0)
					} else {
						result.InternetCheck = testInternetCheck("socks", cfg.SocksPort)
					}
				}
				if ev.Name == "captcha_required" {
					parts := strings.Split(ev.Data, "|")
					if len(parts) >= 1 {
						fmt.Printf("  [!] Капча требуется (режим: %s)\n", parts[0])
					}
				}
				if ev.Name == "workers_completed" {
					done = true
				}
			case core.EventStats:
				if ev.Workers > 0 && !workersPassed {
					workersPassed = true
					result.Workers = ev.Workers
					fmt.Printf("  [✓] Workers: %d\n", ev.Workers)
				}
			case core.EventError:
				fmt.Printf("  [✗] Ошибка ядра: %s\n", ev.Message)
				done = true
			}
		}
	}

	c.Stop()
	drained := make(chan struct{})
	go func() {
		for range events {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
	}

	if wgApplied {
		if cfg.Mode == "socks" && wr != nil {
			_ = wr.Stop()
		} else {
			_ = teardownWG()
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !vkAuthPassed {
		fmt.Printf("  [✗] VKAuth\n")
	}
	if !workersPassed {
		fmt.Printf("  [✗] Workers: 0\n")
	}

	allPassed := vkAuthPassed && connectPassed && workersPassed && result.InternetCheck == "✓"
	if allPassed {
		fmt.Printf("  → PASS\n\n")
	} else {
		fmt.Printf("  → FAIL\n\n")
	}

	return result
}

func testInternetCheck(mode string, socksPort int) string {
	hosts := []string{"8.8.8.8", "1.1.1.1"}

	if mode == "tun" {
		for _, host := range hosts {
			cmd := exec.Command("ping", "-c", "1", "-W", "3", "-I", wgIface, host)
			if cmd.Run() == nil {
				fmt.Printf("  [✓] InternetCheck (ping %s)\n", host)
				return "✓"
			}
		}
		fmt.Printf("  [✗] InternetCheck\n")
		return "✗"
	}

	for _, host := range hosts {
		cmd := exec.Command("curl", "--socks5", fmt.Sprintf("127.0.0.1:%d", socksPort),
			"-sS", "--connect-timeout", "5", "-o", os.DevNull,
			fmt.Sprintf("http://%s", host))
		if cmd.Run() == nil {
			fmt.Printf("  [✓] InternetCheck (socks5 → %s)\n", host)
			return "✓"
		}
	}
	fmt.Printf("  [✗] InternetCheck\n")
	return "✗"
}
