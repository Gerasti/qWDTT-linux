package core

import (
	"net"
	"strings"
	"sync"
)

// Router is the application-level bypass selector used by the SOCKS5 dial
// interceptor. It is the replacement for the inline shouldBypass logic that
// used to live in WireproxyRunner. Matching semantics are preserved 1:1:
//
//   - domains (FQDN) -> suffix match (exact or "<sub>.<dom>")
//   - IPs            -> exact match (a bare IP is treated as a /32)
//   - CIDRs          -> net.IPNet.Contains
//
// Behavioural note (bugfix vs. the old shouldBypass): CIDR entries in the IP
// list are now matched with Contains, so e.g. "1.2.3.87" really matches
// "1.2.3.0/24". The previous exact-string comparison could never match a host
// inside a CIDR, so such entries were silently ignored.
//
// Router is thread-safe: Update swaps the snapshot under a write lock and
// Resolve takes a read lock. It performs no network I/O.
type Router struct {
	mu         sync.RWMutex
	domains    []string    // lowercased bypass domains (suffix-matched)
	ipMatchers []net.IPNet // parsed CIDRs / bare IPs as /32-or-/128
}

// NewRouter builds a Router seeded from the existing domains/ips lists.
func NewRouter(domains, ips []string) *Router {
	r := &Router{}
	r.Update(domains, ips)
	return r
}

// Update hot-reloads the bypass lists without restarting the SOCKS5 server.
// Called by WireproxyRunner.SetBypass (BL_RELOAD / live reconfiguration).
func (r *Router) Update(domains, ips []string) {
	dcopy := make([]string, len(domains))
	for i, d := range domains {
		dcopy[i] = strings.ToLower(strings.TrimSpace(d))
	}
	nets := make([]net.IPNet, 0, len(ips))
	for _, ip := range ips {
		if n, ok := parseIPNetEntry(ip); ok {
			nets = append(nets, n)
		}
	}
	r.mu.Lock()
	r.domains = dcopy
	r.ipMatchers = nets
	r.mu.Unlock()
}

// Resolve decides which upstream a SOCKS5 connection to host must use.
// It returns ("direct", true) when host matches an explicit bypass rule, or
// ("tun", false) when the connection must go through the wireproxy TUN.
// host is the original destination: the FQDN when the client sent one,
// otherwise the IP literal the server resolved.
func (r *Router) Resolve(host string) (upstream string, matched bool) {
	if host == "" {
		return "tun", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	// IP-literal path: match against CIDR/exact-IP matchers.
	if ip := net.ParseIP(host); ip != nil {
		for _, n := range r.ipMatchers {
			if n.Contains(ip) {
				return "direct", true
			}
		}
		return "tun", false
	}

	// Domain path: suffix match. A trailing dot (FQDN form) is tolerated.
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range r.domains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return "direct", true
		}
	}
	return "tun", false
}

// SnapshotDomains returns a consistent copy of the current bypass domains
// (FQDN entries only). Used for read-only introspection over the control
// socket (GET_BL). IP/CIDR entries are intentionally not exposed.
func (r *Router) SnapshotDomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.domains))
	copy(out, r.domains)
	return out
}

// parseIPNetEntry parses a CIDR or a bare IP into a masked IPNet so that
// Contains() is exact. Mirrors the fallback idiom in bypass_routes_linux.go
// (parseRouteDst), kept local so this file has no linux-only dependency.
func parseIPNetEntry(s string) (net.IPNet, bool) {
	if _, ipnet, err := net.ParseCIDR(s); err == nil && ipnet != nil {
		return *ipnet, true
	}
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return net.IPNet{}, false
	}
	mask := net.CIDRMask(32, 32)
	if ip.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}
	return net.IPNet{IP: ip.Mask(mask), Mask: mask}, true
}
