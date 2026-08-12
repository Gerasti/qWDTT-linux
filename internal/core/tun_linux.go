//go:build linux

package core

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const RawIfaceName = "qwdtt-raw"

// rawNetworking хранит state, добавленный setupRawTUN, чтобы
// teardownRawTUN мог очистить всё в обратном порядке.
var rawNetworking struct {
	mu       sync.Mutex
	iface    string
	addr     string // назначенный сервером raw IP (из RAWCONF)
	routes   []string
	mssRules int
}

func createRawTUN(name string) (*os.File, error) {
	nfd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		unix.Close(nfd)
		return nil, fmt.Errorf("NewIfreq: %w", err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(nfd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(nfd)
		return nil, fmt.Errorf("TUNSETIFF: %w", err)
	}

	if err := unix.SetNonblock(nfd, false); err != nil {
		unix.Close(nfd)
		return nil, fmt.Errorf("SetNonblock: %w", err)
	}

	f := os.NewFile(uintptr(nfd), "/dev/net/tun")
	rawNetworking.mu.Lock()
	rawNetworking.iface = name
	rawNetworking.routes = nil
	rawNetworking.addr = ""
	rawNetworking.mssRules = 0
	rawNetworking.mu.Unlock()
	return f, nil
}

// setupRawTUN назначает IP (из RAWCONF), MTU, поднимает интерфейс и ставит
// маршруты полного туннеля. IP берётся в /32 — только наш адрес локален
func setupRawTUN(name, addr string, mtu int) error {
	if ip := net.ParseIP(addr); ip == nil || ip.To4() == nil {
		return fmt.Errorf("некорректный raw IP %q", addr)
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("lookup %s: %w", name, err)
	}

	if mtu > 0 {
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("mtu %d: %w", mtu, err)
		}
	}

	ipnet := &net.IPNet{IP: net.ParseIP(addr), Mask: net.CIDRMask(32, 32)}
	if err := netlink.AddrAdd(link, &netlink.Addr{IPNet: ipnet}); err != nil {
		return fmt.Errorf("addr add %s: %w", addr, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("link set up: %w", err)
	}

	// Маршруты полного туннеля: 0.0.0.0/1 + 128.0.0.0/1 (как в applyWGConfig)
	idx := link.Attrs().Index
	var added []string
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		_, dst, perr := net.ParseCIDR(cidr)
		if perr != nil {
			continue
		}
		r := &netlink.Route{LinkIndex: idx, Dst: dst}
		if err := netlink.RouteAdd(r); err != nil && !os.IsExist(err) {
			return fmt.Errorf("route add %s: %w", cidr, err)
		}
		added = append(added, cidr)
	}

	rawNetworking.mu.Lock()
	rawNetworking.addr = addr
	rawNetworking.routes = added
	rawNetworking.mssRules = setupRawMSSClamping(name)
	rawNetworking.mu.Unlock()

	log.Printf("[CORE] Raw TUN %s: ip=%s mtu=%d routes=%v", name, addr, mtu, added)
	return nil
}

// teardownRawTUN удаляет маршруты, адрес, mangle-правила и сам интерфейс.
func teardownRawTUN() error {
	rawNetworking.mu.Lock()
	name := rawNetworking.iface
	addr := rawNetworking.addr
	routes := rawNetworking.routes
	mssRules := rawNetworking.mssRules
	rawNetworking.iface = ""
	rawNetworking.addr = ""
	rawNetworking.routes = nil
	rawNetworking.mssRules = 0
	rawNetworking.mu.Unlock()

	if name == "" {
		return nil
	}
	link, err := netlink.LinkByName(name)
	if err != nil || link == nil {
		teardownRawMSSClamping(name, mssRules)
		return nil
	}
	idx := link.Attrs().Index
	for _, cidr := range routes {
		if _, dst, perr := net.ParseCIDR(cidr); perr == nil {
			_ = netlink.RouteDel(&netlink.Route{LinkIndex: idx, Dst: dst})
		}
	}
	if addr != "" {
		if ip := net.ParseIP(addr); ip != nil {
			ipnet := &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
			_ = netlink.AddrDel(link, &netlink.Addr{IPNet: ipnet})
		}
	}
	_ = netlink.LinkDel(link)
	teardownRawMSSClamping(name, mssRules)
	log.Printf("[CORE] Raw TUN %s удалён", name)
	return nil
}

// commandExists проверяет наличие бинаря в PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func setupRawMSSClamping(iface string) int {
	n := 0
	if commandExists("iptables") {
		for _, dir := range []string{"-o"} {
			for _, action := range []string{"-D", "-I"} {
				err := exec.Command("iptables", "-t", "mangle", action, "OUTPUT",
					dir, iface, "-p", "tcp", "-m", "tcp", "--tcp-flags", "SYN,RST", "SYN",
					"-m", "comment", "--comment", "WDTT_RAW_MSS",
					"-j", "TCPMSS", "--clamp-mss-to-pmtu").Run()
				_ = err
				if action == "-I" {
					n++
				}
			}
		}
	}
	return n
}

// teardownRawMSSClamping убирает правила MSS, добавленные setupRawMSSClamping.
func teardownRawMSSClamping(iface string, n int) {
	if !commandExists("iptables") {
		return
	}
	for i := 0; i < n; i++ {
		_ = exec.Command("iptables", "-t", "mangle", "-D", "OUTPUT",
			"-o", iface, "-p", "tcp", "-m", "tcp", "--tcp-flags", "SYN,RST", "SYN",
			"-m", "comment", "--comment", "WDTT_RAW_MSS",
			"-j", "TCPMSS", "--clamp-mss-to-pmtu").Run()
	}
}

