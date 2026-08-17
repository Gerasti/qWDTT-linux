//go:build linux

package core

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const RawIfaceName = "raw-qwdtt"

const (
	nftTableName = "wdtt_raw"
	nftChainName = "output_mss"
)

// rawNetworking хранит state, добавленный setupRawTUN, чтобы
// teardownRawTUN мог очистить всё в обратном порядке.
var rawNetworking struct {
	mu     sync.Mutex
	iface  string
	addr   string // назначенный сервером raw IP (из RAWCONF)
	routes []string
	mssSet bool
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
	rawNetworking.mssSet = false
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

	if err := setupRawMSSClampingNFT(name); err != nil {
		if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "no such file or directory") {
			return fmt.Errorf("mss clamp: %w", err)
		}
		log.Printf("[CORE] Raw TUN %s: nftables MSS clamping unavailable (kernel/nftables not supported), continuing without", name)
	}

	rawNetworking.mu.Lock()
	rawNetworking.addr = addr
	rawNetworking.routes = added
	rawNetworking.mssSet = true
	rawNetworking.mu.Unlock()

	log.Printf("[CORE] Raw TUN %s: ip=%s mtu=%d routes=%v", name, addr, mtu, added)
	return nil
}

// teardownRawTUN удаляет маршруты, адрес, nftables правила и сам интерфейс.
func teardownRawTUN() error {
	rawNetworking.mu.Lock()
	name := rawNetworking.iface
	addr := rawNetworking.addr
	routes := rawNetworking.routes
	mssSet := rawNetworking.mssSet
	rawNetworking.iface = ""
	rawNetworking.addr = ""
	rawNetworking.routes = nil
	rawNetworking.mssSet = false
	rawNetworking.mu.Unlock()

	if name == "" {
		return nil
	}

	if mssSet {
		teardownRawMSSClampingNFT()
	}

	link, err := netlink.LinkByName(name)
	if err != nil || link == nil {
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
	log.Printf("[CORE] Raw TUN %s удалён", name)
	return nil
}

// ifname форматирует имя интерфейса как nftables ожидает для сравнения
// в meta oifname: 16-байтный буфер с завершающим нулём (IFNAMSIZ).
func ifname(n string) []byte {
	b := make([]byte, 16)
	copy(b, n+"\x00")
	return b
}

// setupRawMSSClampingNFT создаёт nftables-правило эквивалентное iptables:
//
//	iptables -t mangle -I OUTPUT -o <iface> -p tcp -m tcp --tcp-flags SYN,RST SYN \
//	    -j TCPMSS --clamp-mss-to-pmtu
//
// Эквивалент на nftables:
//
//	nft add table ip wdtt_raw
//	nft add chain ip wdtt_raw output_mss { type filter hook output priority mangle \; }
//	nft add rule  ip wdtt_raw output_mss oifname <iface> tcp flags syn / syn,rst \
//	    tcp option maxseg size set rt mtu
//
// В ядре (nft_exthdr.c, case TCPOPT_MSS) при выполнении exthdr-set для опции
// MSS автоматически применяется min(current_mss, new_value): запись происходит
// только если новое значение меньше текущего. Поэтому min() реализовывать в
// userspace не нужно.
func setupRawMSSClampingNFT(iface string) error {
	// Фаза 1: очистка — удаляем таблицу, если она осталась от предыдущего процесса
	// (например, после аварийного завершения). ENOENT (таблица не существует) здесь
	// ожидаем и игнорируем. DelTable выполняется в отдельном Flush(), иначе ENOENT
	// от него прерывает весь batch и AddTable никогда не выполняется.
	{
		cleanup := &nftables.Conn{}
		cleanup.DelTable(&nftables.Table{
			Family: nftables.TableFamilyIPv4,
			Name:   nftTableName,
		})
		if err := cleanup.Flush(); err != nil {
			if !errors.Is(err, os.ErrNotExist) &&
				!strings.Contains(err.Error(), "no such file or directory") {
				return fmt.Errorf("nft flush (cleanup): %w", err)
			}
		}
	}

	// Фаза 2: создание таблицы, цепочки и правила в одном batch.
	c := &nftables.Conn{}

	table := c.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	chain := c.AddChain(&nftables.Chain{
		Name:     nftChainName,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityMangle,
	})

	c.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// Match: output interface == iface
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(iface)},

			// Match: L4 protocol == TCP
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_TCP}},

			// Match: TCP flags & (SYN|RST) == SYN — только чистые SYN пакеты
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 13, Len: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 1, Mask: []byte{0x06}, Xor: []byte{0x00}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{0x02}},

			// Action: установить TCP опцию 2 (MSS) из маршрута rt tcpmss.
			// Ядро сравнит с текущим значением и запишет только если new < old.
			&expr.Rt{Register: 1, Key: expr.RtTCPMSS},
			&expr.Exthdr{
				SourceRegister: 1,
				Type:           2, // TCPOPT_MSS
				Offset:         2, // пропустить kind(1) + len(1)
				Len:            2,
				Op:             expr.ExthdrOpTcpopt,
			},
		},
	})

	if err := c.Flush(); err != nil {
		return fmt.Errorf("nft flush: %w", err)
	}
	log.Printf("[CORE] nftables MSS clamping rule applied for %s", iface)
	return nil
}

// teardownRawMSSClampingNFT удаляет таблицу целиком (вместе с chain и правилом).
func teardownRawMSSClampingNFT() {
	c := &nftables.Conn{}
	c.DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})
	if err := c.Flush(); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") {
			log.Printf("[CORE] nft teardown: table already gone (ok)")
		} else {
			log.Printf("[CORE] nft teardown: %v", err)
		}
	}
}
