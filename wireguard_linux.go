package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const wgIface = "wg-qwdtt"

var routedTurnIPs []string
var splitRoutes []string

// splitTunnelConfig описывает режим split tunneling для TUN-интерфейса.
//   - "whitelist": только указанные домены идут через туннель, остальное — напрямую.
//   - "blacklist":  всё идёт через туннель, указанные домены — напрямую.
type splitTunnelConfig struct {
	mode    string
	domains []string
	rawBl   string
	rawFile string
}

// splitCacheEntry кеширует резольв доменов в IP на диск.
type splitCacheEntry struct {
	Key     string   `json:"key"`
	Domains []string `json:"domains"`
	IPv4s   []string `json:"ipv4s"`
	Expires int64    `json:"expires"` // unix-секунды
}

const splitCacheTTL = time.Hour
const splitCacheFilename = "split_cache.json"

var splitCacheMu sync.Mutex
var splitCacheInMemory splitCacheEntry

func splitCachePath() string {
	return filepath.Join(pidFilesDir(), splitCacheFilename)
}

func cacheKey(domains []string) string {
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(h[:])
}

func loadSplitCache(key string) (splitCacheEntry, bool) {
	data, err := os.ReadFile(splitCachePath())
	if err != nil {
		return splitCacheEntry{}, false
	}
	var entry splitCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return splitCacheEntry{}, false
	}
	if entry.Key != key || time.Now().Unix() > entry.Expires {
		return splitCacheEntry{}, false
	}
	return entry, true
}

func saveSplitCache(entry splitCacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(splitCachePath(), data, 0o600)
}

// resolveDomainIPs резолвит домены в IPv4 через системный резолвер (НЕ через
// DoH), используя in-memory + file кеш. Возвращает список уникальных IP.
func resolveDomainIPs(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("no domains provided")
	}
	key := cacheKey(domains)

	splitCacheMu.Lock()
	if splitCacheInMemory.Key == key && time.Now().Unix() < splitCacheInMemory.Expires && len(splitCacheInMemory.IPv4s) > 0 {
		ips := splitCacheInMemory.IPv4s
		splitCacheMu.Unlock()
		return ips, nil
	}
	splitCacheMu.Unlock()

	if entry, ok := loadSplitCache(key); ok {
		splitCacheMu.Lock()
		splitCacheInMemory = entry
		splitCacheMu.Unlock()
		return entry.IPv4s, nil
	}

	resolver := &net.Resolver{PreferGo: true}
	seen := make(map[string]struct{})
	var ips []string
	for _, d := range domains {
		if ip := net.ParseIP(d); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				s := ip4.String()
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					ips = append(ips, s)
				}
			}
			continue
		}
		if _, _, err := net.ParseCIDR(d); err == nil {
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				ips = append(ips, d)
			}
			continue
		}
		addrs, err := resolver.LookupIPAddr(context.Background(), d)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ip := addr.IP.To4(); ip != nil {
				s := ip.String()
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					ips = append(ips, s)
				}
			}
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for domains: %s", strings.Join(domains, ", "))
	}

	entry := splitCacheEntry{
		Key:     key,
		Domains: domains,
		IPv4s:   ips,
		Expires: time.Now().Add(splitCacheTTL).Unix(),
	}
	splitCacheMu.Lock()
	splitCacheInMemory = entry
	splitCacheMu.Unlock()
	saveSplitCache(entry)
	return ips, nil
}

// toCIDRRoute converts a plain IP to a /32 CIDR, or returns CIDR strings as-is.
func toCIDRRoute(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	return ip + "/32"
}


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

func applyWGConfig(config string, turnIPs []string, splitCfg *splitTunnelConfig) error {
	addr, mtu, cleanConfig := parseWGConfig(config)

	if addr == "" {
		return fmt.Errorf("no Address found in config")
	}

	// Резолв доменов для split tunneling (кешируется на диск + in-memory)
	var splitIPs []string
	if splitCfg != nil && len(splitCfg.domains) > 0 {
		ips, err := resolveDomainIPs(splitCfg.domains)
		if err != nil {
			fmt.Printf("[WARNING] Не удалось резолвить домены для split tunneling: %v\n", err)
		} else {
			splitIPs = ips
		}
	}

	if splitCfg != nil && splitCfg.mode == "blacklist" && len(splitIPs) > 0 {
		fmt.Printf("[*] Split tunnel (blacklist): %d IP напрямую\n", len(splitIPs))
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
		cmd.Run()
	}

	gatewayCmd := exec.Command("sh", "-c", "ip route | grep default | awk '{print $3}' | head -n1")
	gatewayOut, err := gatewayCmd.Output()
	gateway := strings.TrimSpace(string(gatewayOut))

	if gateway != "" && err == nil {
		routedTurnIPs = turnIPs
		for _, turnIP := range turnIPs {
			if turnIP != "" {
				cmd = exec.Command("ip", "route", "replace", turnIP, "via", gateway)
				if err := cmd.Run(); err != nil {
					fmt.Printf("Info: route for %s: %v\n", turnIP, err)
				}
			}
		}

		// Blacklist: более специфичные маршруты через прямой шлюз (обход туннеля)
		if splitCfg != nil && splitCfg.mode == "blacklist" && len(splitIPs) > 0 {
			for _, ip := range splitIPs {
				cidr := toCIDRRoute(ip)
				cmd = exec.Command("ip", "route", "replace", cidr, "via", gateway)
				if err := cmd.Run(); err != nil {
					fmt.Printf("Warning: не удалось добавить bypass-маршрут %s: %v\n", cidr, err)
					continue
				}
				splitRoutes = append(splitRoutes, cidr)
			}
		}
	}

	// Полный туннель (по умолчанию)
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		cmd = exec.Command("ip", "route", "add", cidr, "dev", wgIface)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: failed to add route %s: %v\n", cidr, err)
		}
	}

	return nil
}

func teardownWG() error {
	for _, cidr := range splitRoutes {
		exec.Command("ip", "route", "del", cidr).Run()
	}
	splitRoutes = nil
	for _, ip := range routedTurnIPs {
		exec.Command("ip", "route", "del", ip).Run()
	}
	routedTurnIPs = nil
	return exec.Command("ip", "link", "del", wgIface).Run()
}

func testWGConnectivity() error {
	cmd := exec.Command("ip", "link", "show", wgIface)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("interface not found")
	}

	testHosts := []string{
		"1.1.1.1",
		"8.8.8.8",
		"208.67.222.222",
	}

	for _, host := range testHosts {
		cmd = exec.Command("ping", "-c", "1", "-W", "3", "-I", wgIface, host)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no connectivity through tunnel")
}
