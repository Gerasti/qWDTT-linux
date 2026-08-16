package core

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Config struct {
	PeerAddr    string
	Password    string
	Hashes      []string
	Listen      string
	TurnHost    string
	TurnPort    string
	DeviceID    string
	Workers     int
	CaptchaMode string
	MTU         int
	DNS         string
	Mode        string
	SocksPort   int
	SocksUser   string
	SocksPass   string
	WGRawConfig string
	RawMode     bool
	NoDTLS      bool
	Transport   string
	RawPort     string
}

type EventType string

const (
	EventState EventType = "state"
	EventLog   EventType = "log"
	EventEvent EventType = "event"
	EventError EventType = "error"
	EventStats EventType = "stats"
)

// DefaultSocksPort is the default SOCKS5 port.
const DefaultSocksPort = 9050

type Event struct {
	Type EventType

	// state
	Status string

	// log
	Level   string
	Message string

	// event
	Name string
	Data string

	// stats
	RxBytes int64
	TxBytes int64
	Workers int32
}

// Core — runtime controller ядра.
type Core struct {
	cfg               Config
	cancel            context.CancelFunc
	pauseFlag         int32
	CaptchaResultChan chan string
	captchaMode       atomic.Value
	events            chan Event
	once              sync.Once
	turnIPsMu         sync.Mutex
	turnIPs           []string
	vkAuthPassed      int32
}

// AddTurnIPs регистрирует TURN IP-адреса (без порта) для исключения из туннеля.
func (c *Core) AddTurnIPs(urls []string) {
	c.turnIPsMu.Lock()
	defer c.turnIPsMu.Unlock()
	seen := make(map[string]struct{}, len(c.turnIPs))
	for _, ip := range c.turnIPs {
		seen[ip] = struct{}{}
	}
	for _, u := range urls {
		host, _, _ := net.SplitHostPort(strings.TrimPrefix(u, "turn:"))
		if host == "" {
			host = u
		}
		if _, ok := seen[host]; !ok {
			seen[host] = struct{}{}
			c.turnIPs = append(c.turnIPs, host)
		}
	}
}

// GetTurnIPs возвращает все зарегистрированные TURN IP.
func (c *Core) GetTurnIPs() []string {
	c.turnIPsMu.Lock()
	defer c.turnIPsMu.Unlock()
	result := make([]string, len(c.turnIPs))
	copy(result, c.turnIPs)
	return result
}

// New создаёт Core. Start() запускает его.
func New(cfg Config) (*Core, error) {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = "unknown"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 9
	}
	if cfg.Mode == "" {
		cfg.Mode = "tun"
	}
	if cfg.SocksPort <= 0 {
		cfg.SocksPort = DefaultSocksPort
	}
	if cfg.Mode != "tun" && cfg.Mode != "socks" && cfg.Mode != "raw" {
		return nil, fmt.Errorf("invalid mode %q: must be \"tun\", \"socks\" or \"raw\"", cfg.Mode)
	}

	t := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if t != "" && t != "udp" && t != "tcp" {
		return nil, fmt.Errorf("invalid transport %q: must be \"udp\" or \"tcp\"", cfg.Transport)
	}
	if t != "tcp" {
		cfg.Transport = "udp"
	}
	c := &Core{
		cfg:               cfg,
		CaptchaResultChan: make(chan string, 1),
		events:            make(chan Event, 256),
	}
	c.captchaMode.Store(normalizeCaptchaMode(cfg.CaptchaMode))
	return c, nil
}

// Start запускает ядро. Возвращает канал событий (закрывается при завершении).
func (c *Core) Start() (<-chan Event, error) {
	dnsArg := c.cfg.DNS
	if dnsArg == "" {
		dnsArg = "yandex"
	}
	setupGlobalResolver(dnsArg)

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	if c.cfg.PeerAddr == "" {
		cancel()
		return nil, fmt.Errorf("PeerAddr is required")
	}
	if len(c.cfg.Hashes) == 0 {
		cancel()
		return nil, fmt.Errorf("Hashes are required")
	}
	if c.cfg.Password == "" {
		cancel()
		return nil, fmt.Errorf("Password is required")
	}

	peer, err := net.ResolveUDPAddr("udp", c.cfg.PeerAddr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("resolve peer: %w", err)
	}

	if c.cfg.RawMode || c.cfg.NoDTLS {
		rawPort := c.cfg.RawPort
		if rawPort == "" {
			rawPort = "56003"
		}
		host := peer.IP.String()
		if peer.IP.To4() == nil {
			host = "[" + peer.IP.String() + "]"
		}
		rawPeer, rerr := net.ResolveUDPAddr("udp", net.JoinHostPort(host, rawPort))
		if rerr != nil {
			cancel()
			return nil, fmt.Errorf("resolve raw peer: %w", rerr)
		}
		peer = rawPeer
		log.Printf("[CORE] Raw/NoDTLS peer: %s", peer.String())
	}

	wrapKey, err := deriveWrapKey(c.cfg.Password)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("derive wrap key: %w", err)
	}

	// Нормализуем количество воркеров
	maxWorkers := 108
	n := c.cfg.Workers
	if n > maxWorkers {
		n = maxWorkers
	}
	if n < workersPerGroup {
		n = workersPerGroup
	}
	n = (n / workersPerGroup) * workersPerGroup

	tp := &TurnParams{
		Host:      c.cfg.TurnHost,
		Port:      c.cfg.TurnPort,
		Hashes:    c.cfg.Hashes,
		WrapKey:   wrapKey,
		ObfsMode:  "audio",
		NoDTLS:    c.cfg.NoDTLS || c.cfg.RawMode,
		RawMode:   c.cfg.RawMode,
		Transport: c.cfg.Transport,
	}

	// В raw-режиме нет UDP loopback (обычно 127.0.0.1:9000 для WireGuard)
	var localConn net.PacketConn
	var tunFd *os.File
	var localPort string

	if c.cfg.RawMode {
		var errTun error
		tunFd, errTun = createRawTUN(RawIfaceName)
		if errTun != nil {
			cancel()
			return nil, fmt.Errorf("create raw tun: %w", errTun)
		}
		localPort = ""
	} else {
		lc := net.ListenConfig{
			Control: func(network, address string, c syscall.RawConn) error {
				var setErr error
				if err := c.Control(func(fd uintptr) {
					setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				}); err != nil {
					return err
				}
				return setErr
			},
		}
		localConn, err = lc.ListenPacket(ctx, "udp", c.cfg.Listen)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("listen %s: %w", c.cfg.Listen, err)
		}
		if uc, ok := localConn.(*net.UDPConn); ok {
			_ = uc.SetReadBuffer(socketBufSize)
			_ = uc.SetWriteBuffer(socketBufSize)
		}
		if addr, ok := localConn.LocalAddr().(*net.UDPAddr); ok {
			localPort = strconv.Itoa(addr.Port)
		}
		if localPort == "" || localPort == "0" {
			localPort = "9000"
		}
	}

	numGroups := n / workersPerGroup

	stats := NewStats()
	emitCaptchaRequest := func(mode, redirectURI, sessionToken string) {
		c.emit(Event{Type: EventEvent, Name: "captcha_required", Data: mode + "|" + redirectURI + "|" + sessionToken})
	}
	emitLog := func(msg string) {
		if strings.Contains(msg, "[VK Auth] Success") {
			c.SetVKAuthPassed()
		}
		c.emit(Event{Type: EventLog, Message: msg})
	}

	shutdownCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(shutdownCh)
	}()
	go stats.RunLoop(shutdownCh)

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-shutdownCh:
				return
			case <-ticker.C:
				c.emit(Event{
					Type:    EventStats,
					RxBytes: stats.TotalBytesDown.Load(),
					TxBytes: stats.TotalBytesUp.Load(),
					Workers: stats.ActiveConnections.Load(),
				})
			}
		}
	}()

	var disp *Dispatcher
	if c.cfg.RawMode {
		disp = NewDispatcherPendingTUN(ctx, stats)
		disp.AttachTUN(tunFd)
		log.Printf("[CORE] Raw-режим: TUN %s создан, диспетчер привязан", RawIfaceName)
	} else {
		disp = NewDispatcher(ctx, localConn, stats)
	}

	configCh := make(chan string, 1)

	go func() {
		select {
		case rawConf, ok := <-configCh:
			if !ok || rawConf == "" {
				return
			}
			if strings.HasPrefix(rawConf, "RAWCONF:") {
				// raw mode: ip|dns|mtu
				parts := strings.Split(strings.TrimPrefix(rawConf, "RAWCONF:"), "|")
				if len(parts) == 3 {
					mtu, _ := strconv.Atoi(parts[2])
					if err := setupRawTUN(RawIfaceName, parts[0], mtu); err != nil {
						log.Printf("[CORE] Ошибка настройки raw TUN: %v", err)
					} else {
						log.Printf("[CORE] Raw TUN готов: ip=%s dns=%s mtu=%d", parts[0], parts[1], mtu)
					}
					c.emit(Event{Type: EventEvent, Name: "raw_conf", Data: fmt.Sprintf("%s|%s|%d", parts[0], parts[1], mtu)})
					c.emit(Event{Type: EventEvent, Name: "raw_ready"})
				}
			} else {
				finalConf := patchWGConfig(rawConf, c.cfg.MTU)
				c.emit(Event{Type: EventEvent, Name: "wg_config", Data: finalConf})
			}
		case <-ctx.Done():
		}
	}()

	c.emit(Event{Type: EventState, Status: "connecting"})

	go func() {
		defer close(c.events)
		defer disp.Shutdown()
		defer cancel()
		if c.cfg.RawMode {
			defer func() {
				if tunFd != nil {
					_ = tunFd.Close()
				}
				_ = teardownRawTUN()
				log.Println("[CORE] Raw TUN teardown complete")
			}()
		} else {
			defer func() {
				_ = localConn.Close()
			}()
		}

		var wg sync.WaitGroup
		workerIDCounter := 1
		var prevWaitReady <-chan struct{}

		for g := 0; g < numGroups; g++ {
			isFirst := g == 0
			var myWaitReady <-chan struct{}
			var mySignalReady chan<- struct{}

			if g > 0 {
				myWaitReady = prevWaitReady
			}
			if g < numGroups-1 {
				ch := make(chan struct{})
				mySignalReady = ch
				prevWaitReady = ch
			}

			ids := make([]int, workersPerGroup)
			for i := range ids {
				ids[i] = workerIDCounter
				workerIDCounter++
			}

			gID := g + 1
			var cc chan<- string
			if isFirst {
				cc = configCh
			}

			wg.Add(1)
			go func(groupID int, isFirstGroup bool, configChan chan<- string, workerIds []int, startHashIndex int, waitR <-chan struct{}, sigR chan<- struct{}) {
				defer wg.Done()
				WorkerGroup(ctx, groupID, startHashIndex, tp, peer, disp, localPort,
					isFirstGroup, configChan, workerIds, &c.pauseFlag,
					c.cfg.DeviceID, c.cfg.Password, stats, waitR, sigR,
					c.CaptchaResultChan, c.getCaptchaMode, emitCaptchaRequest, c.AddTurnIPs, emitLog)
			}(gID, isFirst, cc, ids, g, myWaitReady, mySignalReady)
		}

		wg.Wait()
		close(configCh)
		log.Println("[CORE] все воркеры завершены")
		c.emit(Event{Type: EventEvent, Name: "workers_completed"})
	}()

	return c.events, nil
}

// Stop останавливает ядро.
func (c *Core) Stop() {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// Pause приостанавливает воркеры.
func (c *Core) Pause() { atomic.StoreInt32(&c.pauseFlag, 1) }

// Resume возобновляет воркеры.
func (c *Core) Resume() { atomic.StoreInt32(&c.pauseFlag, 0) }

// SolveCaptcha передаёт токен капчи в ядро.
func (c *Core) SolveCaptcha(token string) {
	select {
	case <-c.CaptchaResultChan:
	default:
	}
	c.CaptchaResultChan <- token
}

func (c *Core) emit(ev Event) {
	select {
	case c.events <- ev:
	default:
		if ev.Type != EventLog {
			// Drain one stale entry to make room for important event
			select {
			case <-c.events:
			default:
			}
			select {
			case c.events <- ev:
			default:
			}
		}
	}
}

// SetVKAuthPassed marks that VKAuth has succeeded at least once.
func (c *Core) SetVKAuthPassed() {
	atomic.StoreInt32(&c.vkAuthPassed, 1)
}

// IsVKAuthPassed reports whether VKAuth has succeeded.
func (c *Core) IsVKAuthPassed() bool {
	return atomic.LoadInt32(&c.vkAuthPassed) == 1
}

func (c *Core) getCaptchaMode() string {
	mode, _ := c.captchaMode.Load().(string)
	if mode == "" {
		return "auto"
	}
	return mode
}

func normalizeCaptchaMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto", "rjs", "wv":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}
