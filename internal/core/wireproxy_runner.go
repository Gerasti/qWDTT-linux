package core

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/bufferpool"
	"github.com/windtf/wireproxy"
	"golang.zx2c4.com/wireguard/device"
)

type WireproxyRunner struct {
	tun           *wireproxy.VirtualTun
	cancel        context.CancelFunc
	socksPort     int
	socksUser     string
	socksPass     string
	socksBindAddr string
	config        *wireproxy.Configuration
	socksListener net.Listener
	router        *Router
}

// SetBypass updates the bypass lists used by the SOCKS5 dial interceptor at
// runtime, without restarting the wireproxy SOCKS5 server. Lists are handed to
// the application-level Router, which re-snapshots them on each dial.
func (w *WireproxyRunner) SetBypass(domains, ips []string) {
	w.router.Update(domains, ips)
}

// Router exposes the bypass Router so the control socket can serve read-only
// GET_BL introspection without re-implementing matching logic.
func (w *WireproxyRunner) Router() *Router {
	return w.router
}

func NewWireproxyRunner(socksPort int, socksUser, socksPass, socksBindAddr string, bypassDomains, bypassIPs []string) *WireproxyRunner {
	if socksBindAddr == "" {
		socksBindAddr = "127.0.0.1"
	}
	return &WireproxyRunner{
		socksPort:     socksPort,
		socksUser:     socksUser,
		socksPass:     socksPass,
		socksBindAddr: socksBindAddr,
		router:        NewRouter(bypassDomains, bypassIPs),
	}
}

// dialHost extracts the host used for bypass matching from a SOCKS5 request.
// FQDN is preferred (lets the Router suffix-match the original destination);
// it falls back to the IP literal the server resolved.
func dialHost(req *socks5.Request) string {
	if req == nil {
		return ""
	}
	if req.RawDestAddr != nil && req.RawDestAddr.FQDN != "" {
		return req.RawDestAddr.FQDN
	}
	if req.DestAddr != nil && len(req.DestAddr.IP) > 0 {
		return req.DestAddr.IP.String()
	}
	return ""
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
	confBuilder.WriteString(fmt.Sprintf("[Socks5]\nBindAddress = %s:%d\n", w.socksBindAddr, w.socksPort))
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
			// restart. The application-level Router.Resolve decides direct vs
			// tunnel per destination (domain suffix / exact IP / CIDR). When
			// the lists are empty, Resolve returns "tun" and all traffic flows
			// through the tunnel — same behavior as the previous plain-dial path.
			directDialer := &net.Dialer{Timeout: 30 * time.Second, Control: nil}
			options = []socks5.Option{
				socks5.WithDialAndRequest(func(ctx context.Context, network, addr string, req *socks5.Request) (net.Conn, error) {
					if up, _ := w.router.Resolve(dialHost(req)); up == "direct" {
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
