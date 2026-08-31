//go:build linux

package main

import (
	"net"
	"testing"
	"time"

	"qwdtt/internal/core"
)

// TestGetBlProtocolRoundTrip exercises the read-only GET_BL control command
// end-to-end (server CaptchaSocket.handleConn + client querySocksBypass)
// over a real unix socket in a temp XDG_RUNTIME_DIR. No WireGuard, no
// network — only verifies the NOT_READY / domain-list / empty-list contract.
func TestGetBlProtocolRoundTrip(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	cs := newCaptchaSocket("testbl")
	if err := cs.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer cs.Stop()
	sk := socketPath("testbl")

	// 1) Until SetRouter is called, GET_BL must answer NOT_READY — and this
	//    is distinct from an empty list (the caller must NOT treat it as "no
	//    domains": it means "the daemon is not ready yet").
	if _, err := querySocksBypass(sk, time.Second); err != errSocksRouterNotReady {
		t.Fatalf("before SetRouter: want errNotReady, got %v", err)
	}

	// 2) After attaching a populated router: domains + END_BL.
	cs.SetRouter(core.NewRouter([]string{"vk.com", "yandex.ru"}, nil))
	got, err := querySocksBypass(sk, time.Second)
	if err != nil {
		t.Fatalf("GET_BL domains: %v", err)
	}
	if len(got) != 2 || got[0] != "vk.com" || got[1] != "yandex.ru" {
		t.Fatalf("GET_BL domains: want [vk.com yandex.ru], got %v", got)
	}

	// 3) A router with no domains is a valid empty list (END_BL at once),
	//    NOT an error and NOT "not ready".
	cs.SetRouter(core.NewRouter(nil, nil))
	got, err = querySocksBypass(sk, time.Second)
	if err != nil {
		t.Fatalf("GET_BL empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GET_BL empty: want 0 domains, got %v", got)
	}
}

// TestQuerySocksBypassBadPeer ensures a non-responding peer degrades to an
// error (so a hung socks daemon cannot stall a kernel connect beyond the
// per-socket deadline), not a hang.
func TestQuerySocksBypassBadPeer(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	sk := socketPath("deadbl")
	ln, err := net.Listen("unix", sk)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if _, err := querySocksBypass(sk, 200*time.Millisecond); err == nil {
		t.Fatalf("want timeout/EOF error from non-responding peer, got nil")
	}
}
