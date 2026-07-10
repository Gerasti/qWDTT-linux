package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const wgIface = "wg-qwdtt"


// wg-quick-only fields that wg setconf doesn't understand
var wgQuickOnlyFields = map[string]bool{
	"address": true, "dns": true, "mtu": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}

// parseWGConfig extracts Address, MTU and returns a wg-setconf-compatible config
func parseWGConfig(conf string) (addr, mtu string, wgConf string) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(conf))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			switch key {
			case "address":
				addr = val
				continue
			case "mtu":
				mtu = val
				continue
			default:
				if wgQuickOnlyFields[key] {
					continue
				}
			}
		}
		out.WriteString(line + "\n")
	}
	wgConf = out.String()
	return
}

func applyWGConfig(config string, turnIPs []string) error {
	addr, mtu, cleanConfig := parseWGConfig(config)

	if addr == "" {
		return fmt.Errorf("no Address found in config")
	}

	if err := exec.Command("ip", "link", "show", wgIface).Run(); err == nil {
		if err := teardownWG(); err != nil {
			return fmt.Errorf("failed to remove existing interface: %w", err)
		}
	}

	cmd := exec.Command("ip", "link", "add", wgIface, "type", "wireguard")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create wireguard interface: %w\n\nRequired capabilities:\n  ip needs cap_net_admin+eip\n\nNixOS setup:\n  security.wrappers.ip = {\n    source = \"${pkgs.iproute2}/bin/ip\";\n    capabilities = \"cap_net_admin+eip\";\n  };", err)
	}

	tmpFile, err := os.CreateTemp("", "wg-*.conf")
	if err != nil {
		teardownWG()
		return fmt.Errorf("failed to create temp config: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(cleanConfig); err != nil {
		tmpFile.Close()
		teardownWG()
		return fmt.Errorf("failed to write config: %w", err)
	}
	tmpFile.Close()

	cmd = exec.Command("wg", "setconf", wgIface, tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		teardownWG()
		return fmt.Errorf("failed to apply config: %w (output: %s)", err, string(output))
	}

	cmd = exec.Command("ip", "link", "set", wgIface, "up")
	if err := cmd.Run(); err != nil {
		teardownWG()
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	cmd = exec.Command("ip", "addr", "add", addr, "dev", wgIface)
	if err := cmd.Run(); err != nil {
		teardownWG()
		return fmt.Errorf("failed to assign IP: %w", err)
	}

	if mtu != "" {
		cmd = exec.Command("ip", "link", "set", wgIface, "mtu", mtu)
		cmd.Run() // игнорируем ошибки MTU
	}

	gatewayCmd := exec.Command("sh", "-c", "ip route | grep default | awk '{print $3}' | head -n1")
	gatewayOut, err := gatewayCmd.Output()
	gateway := strings.TrimSpace(string(gatewayOut))

	if gateway != "" && err == nil {
		for _, turnIP := range turnIPs {
			if turnIP != "" {
				cmd = exec.Command("ip", "route", "add", turnIP, "via", gateway)
				if err := cmd.Run(); err != nil {
					fmt.Printf("Info: route for %s: %v\n", turnIP, err)
				}
			}
		}
	}

	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		cmd = exec.Command("ip", "route", "add", cidr, "dev", wgIface)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: failed to add route %s: %v\n", cidr, err)
		}
	}

	return nil
}

func teardownWG() error {
	cmd := exec.Command("ip", "link", "del", wgIface)
	if err := cmd.Run(); err != nil {
		return nil
	}
	return nil
}
