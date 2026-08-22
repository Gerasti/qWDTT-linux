package core

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/bufferpool"
	"github.com/windtf/wireproxy"
	"golang.zx2c4.com/wireguard/device"
)

type WireproxyRunner struct {
	mu            sync.RWMutex
	tun           *wireproxy.VirtualTun
	cancel        context.CancelFunc
	socksPort     int
	socksUser     string
	socksPass     string
	config        *wireproxy.Configuration
	socksListener net.Listener
	bypassDomains []string
	bypassIPs     []string
}

// SetBypass updates the bypass lists used by the SOCKS5 dial interceptor at
// runtime, without restarting the wireproxy SOCKS5 server. Domains are matched
// by suffix on each new connection; IPs give an exact-match shortcut.
func (w *WireproxyRunner) SetBypass(domains, ips []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bypassDomains = domains
	w.bypassIPs = ips
}

// bypassSnapshot returns a consistent copy of the current bypass lists.
func (w *WireproxyRunner) bypassSnapshot() (domains, ips []string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.bypassDomains, w.bypassIPs
}

func NewWireproxyRunner(socksPort int, socksUser, socksPass string, bypassDomains, bypassIPs []string) *WireproxyRunner {
	return &WireproxyRunner{
		socksPort:     socksPort,
		socksUser:     socksUser,
		socksPass:     socksPass,
		bypassDomains: bypassDomains,
		bypassIPs:     bypassIPs,
	}
}

// shouldBypass returns true if the SOCKS request destination matches
// a blacklisted domain (FQDN suffix) or a pre-resolved bypass IP.
func shouldBypass(req *socks5.Request, domains, ips []string) bool {
	if req == nil {
		return false
	}

	if req.RawDestAddr != nil && req.RawDestAddr.FQDN != "" {
		fqdn := strings.ToLower(strings.TrimSuffix(req.RawDestAddr.FQDN, "."))
		for _, d := range domains {
			dl := strings.ToLower(strings.TrimSpace(d))
			if dl == "" {
				continue
			}
			if fqdn == dl || strings.HasSuffix(fqdn, "."+dl) {
				return true
			}
		}
	}

	if req.DestAddr != nil && len(req.DestAddr.IP) > 0 {
		ipStr := req.DestAddr.IP.String()
		for _, ip := range ips {
			if ip == ipStr {
				return true
			}
		}
	}

	return false
}

func (w *WireproxyRunner) Start(ctx context.Context, wgConfig string) error {
	// Write the WG config to a temp file
	tmpFile, err := os.CreateTemp("", "qwdtt-wg-*.conf")
	if err != nil {
		return fmt.Errorf("failed to create temp config: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(wgConfig); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	tmpFile.Close()

	// Create wireproxy config directory
	configDir := filepath.Dir(tmpFile.Name())
	wireproxyConfigPath := filepath.Join(configDir, "wireproxy.conf")

	// Build wireproxy config that references the WG config
	var confBuilder strings.Builder
	confBuilder.WriteString(fmt.Sprintf("WGConfig = %s\n", tmpFile.Name()))
	confBuilder.WriteString(fmt.Sprintf("[Socks5]\nBindAddress = 127.0.0.1:%d\n", w.socksPort))
	if w.socksUser != "" {
		confBuilder.WriteString(fmt.Sprintf("Username = %s\n", w.socksUser))
		confBuilder.WriteString(fmt.Sprintf("Password = %s\n", w.socksPass))
	}

	if err := os.WriteFile(wireproxyConfigPath, []byte(confBuilder.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write wireproxy config: %w", err)
	}
	defer os.Remove(wireproxyConfigPath)

	// Parse the wireproxy config
	conf, err := wireproxy.ParseConfig(wireproxyConfigPath)
	if err != nil {
		return fmt.Errorf("failed to parse wireproxy config: %w", err)
	}

	w.config = conf

	// Start WireGuard using wireproxy's pure-Go implementation
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	tun, err := wireproxy.StartWireguard(conf, device.LogLevelSilent)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to start wireguard: %w", err)
	}

	w.tun = tun

	// Spawn SOCKS5 server (manually, to keep listener ref for cleanup)
	for _, spawner := range conf.Routines {
		if socksCfg, ok := spawner.(*wireproxy.Socks5Config); ok {
			var authMethods []socks5.Authenticator
			if username := socksCfg.Username; username != "" {
				authMethods = []socks5.Authenticator{
					socks5.UserPassAuthenticator{
						Credentials: socks5.StaticCredentials{socksCfg.Username: socksCfg.Password},
					},
				}
			} else {
				authMethods = []socks5.Authenticator{socks5.NoAuthAuthenticator{}}
			}

			var options []socks5.Option

			// Always install the bypass-aware dial handler so that hot-reloaded
			// bypass lists (via SetBypass / bl load) take effect without a
			// restart. When the lists are empty, shouldBypass returns false and
			// all traffic flows through the tunnel — same behavior as the
			// previous plain-dial path.
			directDialer := &net.Dialer{Timeout: 30 * time.Second, Control: nil}
			options = []socks5.Option{
				socks5.WithDialAndRequest(func(ctx context.Context, network, addr string, req *socks5.Request) (net.Conn, error) {
					if bd, bip := w.bypassSnapshot(); shouldBypass(req, bd, bip) {
						return directDialer.DialContext(ctx, network, addr)
					}
					return tun.Tnet.DialContext(ctx, network, addr)
				}),
				socks5.WithResolver(tun),
				socks5.WithAuthMethods(authMethods),
				socks5.WithBufferPool(bufferpool.NewPool(256 * 1024)),
			}

			server := socks5.NewServer(options...)

			ln, err := net.Listen("tcp", socksCfg.BindAddress)
			if err != nil {
				return fmt.Errorf("failed to listen on %s: %w", socksCfg.BindAddress, err)
			}
			w.socksListener = ln

			go func() {
				server.Serve(ln)
			}()
		} else {
			go spawner.SpawnRoutine(tun)
		}
	}

	return nil
}

func (w *WireproxyRunner) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	if w.socksListener != nil {
		w.socksListener.Close()
	}
	if w.tun != nil && w.tun.Dev != nil {
		w.tun.Dev.Close()
	}
	return nil
}
