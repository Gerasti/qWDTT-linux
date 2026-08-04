package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/windtf/wireproxy"
	"golang.zx2c4.com/wireguard/device"
)

type WireproxyRunner struct {
	tun       *wireproxy.VirtualTun
	cancel    context.CancelFunc
	socksPort int
	config    *wireproxy.Configuration
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

	// Spawn SOCKS5 server
	for _, spawner := range conf.Routines {
		go spawner.SpawnRoutine(tun)
	}

	return nil
}

func (w *WireproxyRunner) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	if w.tun != nil && w.tun.Dev != nil {
		w.tun.Dev.Close()
	}
	return nil
}
