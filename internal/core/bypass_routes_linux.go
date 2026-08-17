//go:build linux

package core

import (
	"net"
	"os"

	"github.com/vishvananda/netlink"
)

// ClearBypassRoutes deletes the given CIDR routes from the kernel routing
// table. Each entry may be a bare IP (auto-converted to /32) or a CIDR string.
// Non-existent routes are silently skipped so the function is safe to call
// multiple times (e.g. daemon cleaned up, CLI fallback re-runs).
func ClearBypassRoutes(cidrs []string) error {
	var lastErr error
	for _, cidr := range cidrs {
		ipnet, ok := parseRouteDst(cidr)
		if !ok {
			continue
		}
		if err := netlink.RouteDel(&netlink.Route{Dst: ipnet}); err != nil {
			if !os.IsNotExist(err) {
				lastErr = err
			}
		}
	}
	return lastErr
}

// parseRouteDst parses a CIDR or bare IP string into an IPNet.
func parseRouteDst(s string) (*net.IPNet, bool) {
	_, ipnet, err := net.ParseCIDR(s)
	if err == nil {
		return ipnet, true
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, false
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}, true
}
