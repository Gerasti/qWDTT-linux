package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"qwdtt/internal/core"
)

const wgIface = "wg-qwdtt"

var routedTurnIPs []string
var splitRoutes []string
var splitRoutesMu sync.Mutex

type splitTunnelConfig struct {
	mode    string
	domains []string
	rawBl   string
	rawFile string
}

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

func toCIDRRoute(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	return ip + "/32"
}

func parseWGConfig(conf string) (addr, mtu string, cfg wgtypes.Config, err error) {
	scanner := bufio.NewScanner(strings.NewReader(conf))
	var cur *wgtypes.PeerConfig
	section := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			cur = nil
			if section == "Peer" {
				cfg.Peers = append(cfg.Peers, wgtypes.PeerConfig{
					ReplaceAllowedIPs: true,
				})
				cur = &cfg.Peers[len(cfg.Peers)-1]
			}
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "address":
			addr = val
		case "mtu":
			mtu = val
		case "privatekey":
			k, e := wgtypes.ParseKey(val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid PrivateKey: %w", e)
			}
			cfg.PrivateKey = &k
		case "presharedkey":
			k, e := wgtypes.ParseKey(val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid PresharedKey: %w", e)
			}
			if cur != nil {
				cur.PresharedKey = &k
			}
		case "listenport":
			p, e := strconv.Atoi(val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid ListenPort %q: %w", val, e)
			}
			port := p
			cfg.ListenPort = &port
		case "publickey":
			k, e := wgtypes.ParseKey(val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid PublicKey: %w", e)
			}
			if cur != nil {
				cur.PublicKey = k
			}
		case "allowedips":
			if cur == nil {
				continue
			}
			for _, s := range strings.Split(val, ",") {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				_, ipnet, e := net.ParseCIDR(s)
				if e != nil {
					return "", "", cfg, fmt.Errorf("wg config: invalid AllowedIPs %q: %w", s, e)
				}
				cur.AllowedIPs = append(cur.AllowedIPs, *ipnet)
			}
		case "endpoint":
			if cur == nil {
				continue
			}
			ua, e := net.ResolveUDPAddr("udp", val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid Endpoint %q: %w", val, e)
			}
			cur.Endpoint = ua
		case "persistentkeepalive":
			if cur == nil {
				continue
			}
			secs, e := strconv.Atoi(val)
			if e != nil {
				return "", "", cfg, fmt.Errorf("wg config: invalid PersistentKeepalive %q: %w", val, e)
			}
			interval := time.Duration(secs) * time.Second
			cur.PersistentKeepaliveInterval = &interval
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", cfg, fmt.Errorf("wg config: %w", err)
	}
	return addr, mtu, cfg, nil
}

// parseIPNet parses an address as a CIDR, or wraps a bare IP as a host route.
func parseIPNet(s string) (*net.IPNet, error) {
	_, ipnet, err := net.ParseCIDR(s)
	if err == nil {
		// host address preserved by parseIPNet
		return ipnet, nil
	}
	bare := net.ParseIP(strings.TrimSpace(s))
	if bare == nil {
		return nil, fmt.Errorf("invalid address %q", s)
	}
	mask := net.CIDRMask(32, 32)
	if bare.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}
	return &net.IPNet{IP: bare, Mask: mask}, nil
}

// cidrNet parses a CIDR/host route into a *net.IPNet for route operations.
func cidrNet(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		// toCIDRRoute already normalized to IP/32, but be defensive.
		return &net.IPNet{IP: net.ParseIP(s), Mask: net.CIDRMask(32, 32)}
	}
	return ipnet
}

// isDefaultRoute reports whether the route is a default route (0.0.0.0/0 or
// ::/0). netlink materialises the kernel default route either as a nil Dst or
// as a /0 network with a non-nil IP, so both shapes must be treated as default.
func isDefaultRoute(r *netlink.Route) bool {
	if r == nil {
		return false
	}
	if r.Dst == nil || r.Dst.IP == nil {
		return true
	}
	ones, bits := r.Dst.Mask.Size()
	return bits > 0 && ones == 0
}

func applyWGConfig(config string, turnIPs []string, splitCfg *splitTunnelConfig) error {
	addr, mtu, wg, err := parseWGConfig(config)
	if err != nil {
		return fmt.Errorf("failed to parse wg config: %w", err)
	}

	if addr == "" {
		return fmt.Errorf("no Address found in config")
	}

	var splitCfgDomains []string
	if splitCfg != nil && splitCfg.mode == "blacklist" && len(splitCfg.domains) > 0 {
		splitCfgDomains = splitCfg.domains
	}

	// If the interface already exists, tear it down first.
	if link, e := netlink.LinkByName(wgIface); e == nil && link != nil {
		if e := teardownWG(); e != nil {
			return fmt.Errorf("failed to remove existing interface: %w", e)
		}
	}

	// Create the wireguard interface (replaces `ip link add ... type wireguard`).
	if err := netlink.LinkAdd(&netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: wgIface}}); err != nil {
		return fmt.Errorf("failed to create wireguard interface: %w\n\nRequired network capabilities:\n  sudo setcap cap_net_admin+eip qwdtt\n\nNixOS setup:\n  services.qwdtt.enable = true;  # включает wrappers.enable = true; по умолчанию", err)
	}

	link, err := netlink.LinkByName(wgIface)
	if err != nil {
		teardownWG()
		return fmt.Errorf("failed to lookup new interface: %w", err)
	}

	// Apply WireGuard keys/peers via wgctrl (replaces `wg setconf`).
	if err := configureWgDevice(&wg); err != nil {
		teardownWG()
		return err
	}

	if err := netlink.LinkSetUp(link); err != nil {
		teardownWG()
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Assign addresses (addr may be comma-separated CIDRs or bare IPs).
	for _, a := range strings.Split(addr, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ipnet, e := parseIPNet(a)
		if e != nil {
			teardownWG()
			return fmt.Errorf("failed to parse address %q: %w", a, e)
		}
		if e := netlink.AddrAdd(link, &netlink.Addr{IPNet: ipnet}); e != nil {
			teardownWG()
			return fmt.Errorf("failed to assign IP %s: %w", ipnet.String(), e)
		}
	}

	if mtu != "" {
		if m, e := strconv.Atoi(mtu); e == nil {
			_ = netlink.LinkSetMTU(link, m)
		}
	}

	// Discover the default gateway (replaces `ip route | grep default | awk '{print $3}'`).
	gateway := defaultGateway()

	if gateway != nil {
		applyTurnIPsBypass(turnIPs, gateway)

		// Blacklist: более специфичные маршруты через прямой шлюз (обход туннеля).
		// Bypass-домены уже запущенных socks-профилей добавляются в connect.go
		// перед вызовом applyWGConfig (см. mergeBypassDomains + fetchActiveSocksBypassDomains),
		// чтобы direct-трафик socks не capture-ился нашим wg-qwdtt 0.0.0.0/1.
		addKernelBypassRoutes(splitCfgDomains) // routes tracked in splitRoutes
	}

	// Полный туннель
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if e := netlink.RouteAdd(&netlink.Route{LinkIndex: link.Attrs().Index, Dst: cidrNet(cidr)}); e != nil {
			fmt.Printf("Warning: failed to add route %s: %v\n", cidr, e)
		}
	}

	return nil
}

// replacing `wg setconf wg-qwdtt <tmpfile>`.
func configureWgDevice(cfg *wgtypes.Config) error {
	w, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("failed to init wgctrl: %w", err)
	}
	defer w.Close()
	if err := w.ConfigureDevice(wgIface, *cfg); err != nil {
		return fmt.Errorf("failed to apply config: %w", err)
	}
	return nil
}

func teardownTunnelRoutes() {
	clearSplitRoutes()
	for _, ip := range routedTurnIPs {
		_ = netlink.RouteDel(&netlink.Route{Dst: cidrNet(toCIDRRoute(ip))})
	}
	routedTurnIPs = nil
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		_, dst, _ := net.ParseCIDR(cidr)
		_ = netlink.RouteDel(&netlink.Route{Dst: dst})
	}
}

func teardownWG() error {
	teardownTunnelRoutes()
	link, err := netlink.LinkByName(wgIface)
	if err != nil {
		return nil
	}
	return netlink.LinkDel(link)
}

func clearSplitRoutes() {
	splitRoutesMu.Lock()
	defer splitRoutesMu.Unlock()
	for _, cidr := range splitRoutes {
		_ = netlink.RouteDel(&netlink.Route{Dst: cidrNet(cidr)})
	}
	splitRoutes = nil
}

// dedupKeepOrder returns the unique items of src, preserving first-seen order.
func dedupKeepOrder(src []string) []string {
	seen := make(map[string]struct{}, len(src))
	out := make([]string, 0, len(src))
	for _, s := range src {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// reloadSplitRoutes re-applies bypass routes for the running daemon from the
// raw -bl value and (optionally) a new bypass routes JSON file, replacing
// whatever was active before. For tun/raw it manipulates kernel routes through
// the default gateway; for socks it updates the live wireproxy runner via the
// activeWireproxy atomic pointer (no restart). Returns a summary string.
func reloadSplitRoutes(mode, blRaw, daemonProfile, newFile string) (string, error) {
	profile := daemonProfile
	if daemonProfile == "autoswitch" {
		profile = getAutoswitchCurrentProfile()
		if profile == "" {
			return "", fmt.Errorf("autoswitch is active, but no current profile is connected")
		}
	}

	domains := splitDomains(blRaw)
	if newFile != "" {
		fileDomains, err := loadBypassRoutesFile(newFile)
		if err != nil {
			return "", fmt.Errorf("bl-file: %w", err)
		}
		domains = append(domains, fileDomains...)
	}
	domains = dedupKeepOrder(domains)

	switch mode {
	case "socks":
		wr := activeWireproxy.Load()
		if wr == nil {
			return "", fmt.Errorf("no active SOCKS5 runner for profile %q", profile)
		}
		// Resolve domains for both wireproxy IP-exact-match AND potential
		// kernel-level bypass routes (when wg-qwdtt is also active).
		bypassIPs, _ := resolveDomainIPs(domains) // per-domain resolution errors are non-fatal
		wr.SetBypass(domains, bypassIPs)

		// When the kernel WireGuard interface (wg-qwdtt) is active — e.g. a tun
		// profile is running alongside this socks process — the kernel routing
		// table captures ALL IPv4 traffic (0.0.0.0/1, 128.0.0.0/1 via wg-qwdtt).
		// The application-level bypass above only prevents wireproxy from sending
		// traffic through ITS tun; the directDialer connections still follow the
		// kernel routing table and would be captured by wg-qwdtt. We therefore
		// also install kernel-level bypass routes via the default gateway.
		var kernelRoutes []string
		if isKernelInterfaceActive() {
			clearKernelBypassRoutes()
			kernelRoutes = addKernelBypassRoutes(domains)
		}
		_ = writeSplitCfg(profile, blRaw, newFile, domains, kernelRoutes)
		return fmt.Sprintf("socks: bypass list updated (%d domains)", len(domains)), nil

	case "tun", "raw":
		// Clear previously tracked routes, then add fresh ones via the shared helper.
		clearKernelBypassRoutes()
		routes := addKernelBypassRoutes(domains)
		_ = writeSplitCfg(profile, blRaw, newFile, domains, routes)
		return fmt.Sprintf("%s: bypass routes updated (%d domains, %d routes)", mode, len(domains), len(routes)), nil

	default:
		return "", fmt.Errorf("unsupported mode %q", mode)
	}
}

func rawIfaceActive() bool {
	_, err := netlink.LinkByName(core.RawIfaceName)
	return err == nil
}

func routeViaGateway(cidr string, gateway net.IP) error {
	return netlink.RouteReplace(&netlink.Route{Dst: cidrNet(cidr), Gw: gateway})
}

func applyTurnIPsBypass(turnIPs []string, gateway net.IP) {
	routedTurnIPs = nil
	for _, turnIP := range turnIPs {
		if turnIP == "" {
			continue
		}
		cidr := toCIDRRoute(turnIP)
		if e := routeViaGateway(cidr, gateway); e != nil {
			fmt.Printf("Info: route for %s: %v\n", turnIP, e)
		} else {
			routedTurnIPs = append(routedTurnIPs, cidr)
		}
	}
}

// defaultGateway находит первый default-маршрут с gateway в таблице маршрутов.
func defaultGateway() net.IP {
	if routes, e := netlink.RouteList(nil, netlink.FAMILY_ALL); e == nil {
		for _, r := range routes {
			if isDefaultRoute(&r) && r.Gw != nil {
				return r.Gw
			}
		}
	}
	return nil
}

// addKernelBypassRoutes resolves the given domains to IPs and adds kernel-level
// bypass routes (via the default gateway) so that traffic to those addresses
// bypasses any active WireGuard tunnel (wg-qwdtt). Used by tun, raw, and socks
// modes whenever a kernel WireGuard interface may also be present.
// Returns the list of successfully added route CIDRs.
func addKernelBypassRoutes(domains []string) []string {
	if len(domains) == 0 {
		return nil
	}

	ips, err := resolveDomainIPs(domains)
	if err != nil {
		fmt.Printf("[WARNING] Не удалось резолвить домены для обхода: %v\n", err)
		return nil
	}
	if len(ips) == 0 {
		return nil
	}

	gateway := defaultGateway()
	if gateway == nil {
		fmt.Println("[WARNING] default gateway не найден, kernel-обход невозможен")
		return nil
	}

	fmt.Printf("[*] Split tunnel (blacklist): %d IP напрямую\n", len(ips))

	splitRoutesMu.Lock()
	defer splitRoutesMu.Unlock()

	var routes []string
	for _, ip := range ips {
		cidr := toCIDRRoute(ip)
		if e := routeViaGateway(cidr, gateway); e != nil {
			fmt.Printf("Warning: не удалось добавить bypass-маршрут %s: %v\n", cidr, e)
			continue
		}
		splitRoutes = append(splitRoutes, cidr)
		routes = append(routes, cidr)
	}
	return routes
}

// clearKernelBypassRoutes removes all kernel bypass routes tracked in splitRoutes.
func clearKernelBypassRoutes() {
	clearSplitRoutes()
}

// mergeBypassDomains unions own with the bypass domains observed from other
// (socks) profiles, de-duplicating while preserving first-seen order via the
// existing dedupKeepOrder helper. Order matters for deterministic route sets.
func mergeBypassDomains(own []string, others ...[]string) []string {
	out := append([]string(nil), own...)
	for _, o := range others {
		out = append(out, o...)
	}
	return dedupKeepOrder(out)
}

var errSocksRouterNotReady = errors.New("socks bypass router not ready")

// fetchActiveSocksBypassDomains queries (read-only) every already-running
// socks-mode daemon for its current bypass domains and returns their union.
// The query is parallel across profiles, bounded by a 2s per-socket deadline
// and a 3s global budget, so a single slow/unresponsive daemon cannot stall a
// kernel (tun/raw) connect. Any single failure is logged and skipped — never
// aborted — matching the "add what we can, don't block connect" contract.
//
// ownProfile is skipped when non-empty (avoids a redundant self-query; the
// Mode=="socks" filter already excludes kernel daemons, so the normal case is
// covered regardless). Known limitation (see README): hot-reload of a socks
// profile after this one-time sync does NOT trigger a re-sync.
func fetchActiveSocksBypassDomains(ownProfile string) []string {
	details := getRunningProfileDetails()
	collected := make([][]string, 0, len(details))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for profile, d := range details {
		if d.Mode != "socks" {
			continue
		}
		if profile == ownProfile {
			continue
		}
		sk := socketPath(profile)
		if !isUnderRuntimeDir(sk) {
			fmt.Printf("[WARNING] bypass sync: profile %q socket %q вне XDG_RUNTIME_DIR — GET_BL пропущен (security)\n",
				profile, sk)
			continue
		}
		wg.Add(1)
		go func(p, sk string) {
			defer wg.Done()
			domains, err := querySocksBypass(sk, 2*time.Second)
			if err != nil {
				if !errors.Is(err, errSocksRouterNotReady) {
					fmt.Printf("[WARNING] bypass sync: profile %q GET_BL: %v\n", p, err)
				}
				return
			}
			if len(domains) == 0 {
				return
			}
			mu.Lock()
			collected = append(collected, domains)
			mu.Unlock()
		}(profile, sk)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		fmt.Printf("[WARNING] bypass sync: global 3s timeout waiting for socks profiles\n")
	}
	var out []string
	for _, d := range collected {
		out = append(out, d...)
	}
	return out
}

// querySocksBypass dials a socks daemon control socket and returns its
// bypass domains via the read-only GET_BL command. An empty list (END_BL
// received immediately) is a valid, non-error answer. NOT_READY signals that
// the daemon has no bypass Router attached yet (e.g. still starting).
func querySocksBypass(socketPath string, timeout time.Duration) ([]string, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintln(conn, "GET_BL"); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(conn)
	var domains []string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "END_BL":
			return domains, nil
		case "NOT_READY":
			return nil, errSocksRouterNotReady
		}
		if line == "" {
			continue
		}
		domains = append(domains, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("unexpected EOF from GET_BL on %s", socketPath)
}

// isUnderRuntimeDir reports whether path lives under the uid-private
// XDG_RUNTIME_DIR. When false the control socket may sit in a world-accessible
// fallback (e.g. /tmp) and is deliberately not queried, so we never leak a
// profile's bypass list to another local user via GET_BL.
func isUnderRuntimeDir(path string) bool {
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		return false
	}
	rd = strings.TrimSuffix(rd, "/")
	return path == rd || strings.HasPrefix(path, rd+"/")
}

// testRawConnectivity пингует внешние хосты через raw TUN интерфейс.
func testRawConnectivity() error {
	link, err := netlink.LinkByName(core.RawIfaceName)
	if err != nil || link == nil {
		return fmt.Errorf("interface %s not found", core.RawIfaceName)
	}

	testHosts := []string{
		"1.1.1.1",
		"8.8.8.8",
		"208.67.222.222",
	}

	for _, host := range testHosts {
		cmd := exec.Command("ping", "-c", "1", "-W", "3", "-I", core.RawIfaceName, host)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no connectivity through %s", core.RawIfaceName)
}

func testWGConnectivity() error {
	link, err := netlink.LinkByName(wgIface)
	if err != nil || link == nil {
		return fmt.Errorf("interface not found")
	}

	testHosts := []string{
		"1.1.1.1",
		"8.8.8.8",
		"208.67.222.222",
	}

	for _, host := range testHosts {
		cmd := exec.Command("ping", "-c", "1", "-W", "3", "-I", wgIface, host)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no connectivity through tunnel")
}
