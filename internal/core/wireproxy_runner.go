package core

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/bufferpool"
	"github.com/windtf/wireproxy"
	"golang.zx2c4.com/wireguard/device"
)

type WireproxyRunner struct {
	tun           *wireproxy.VirtualTun
	cancel        context.CancelFunc
	socksPort     int
	config        *wireproxy.Configuration
	socksListener net.Listener
}

func NewWireproxyRunner(socksPort int) *WireproxyRunner {
	return &WireproxyRunner{socksPort: socksPort}
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

			options := []socks5.Option{
				socks5.WithDial(tun.Tnet.DialContext),
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
