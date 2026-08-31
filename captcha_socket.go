package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"qwdtt/internal/core"
)

type CaptchaSocket struct {
	path     string
	listener net.Listener
	solveCh  chan string
	closed   bool
	// router, when set (socks-mode daemons only), backs the read-only GET_BL
	// control command so that concurrent kernel (raw/tun) daemons can discover
	// this socks profile's bypass domains. It is nil until SetRouter is called,
	// which means GET_BL answers NOT_READY until then (avoids the race where a
	// kernel daemon queries the socket before the socks router is initialized).
	router atomic.Pointer[core.Router]
}

func newCaptchaSocket(profile string) *CaptchaSocket {
	return &CaptchaSocket{
		path:    socketPath(profile),
		solveCh: make(chan string, 1),
	}
}

// SetRouter attaches the socks-mode bypass Router so the control socket can
// serve read-only GET_BL introspection. Called once after the WireproxyRunner
// (and its Router) is created; before that, GET_BL returns NOT_READY. In
// kernel (tun/raw) daemons no router is ever attached.
func (cs *CaptchaSocket) SetRouter(r *core.Router) {
	cs.router.Store(r)
}

func (cs *CaptchaSocket) Start() error {
	// Clean up any stale socket
	_ = os.Remove(cs.path)

	ln, err := net.Listen("unix", cs.path)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", cs.path, err)
	}
	cs.listener = ln

	// Set permissions so user can connect
	_ = os.Chmod(cs.path, 0o666)

	go cs.acceptLoop()
	return nil
}

func (cs *CaptchaSocket) acceptLoop() {
	for {
		conn, err := cs.listener.Accept()
		if err != nil {
			return
		}
		go cs.handleConn(conn)
	}
}

func (cs *CaptchaSocket) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "CAPTCHA_RESULT|") {
			result := strings.TrimPrefix(line, "CAPTCHA_RESULT|")
			select {
			case cs.solveCh <- result:
			default:
			}
			break
		}
		if strings.HasPrefix(line, "BL_RELOAD|") {
			newFile := strings.TrimPrefix(line, "BL_RELOAD|")
			fmt.Fprintf(conn, "%s\n", handleBlReload(newFile))
			break
		}
		if line == "GET_BL" {
			// Read-only introspection used by kernel (raw/tun) daemons to
			// discover this socks profile's current bypass domains. NOT_READY
			// (router not attached yet) is intentionally distinct from an empty
			// list (END_BL immediately): it signals "try again later" rather
			// than "no domains".
			r := cs.router.Load()
			if r == nil {
				fmt.Fprintf(conn, "NOT_READY\n")
				break
			}
			for _, d := range r.SnapshotDomains() {
				fmt.Fprintf(conn, "%s\n", d)
			}
			fmt.Fprintf(conn, "END_BL\n")
			break
		}
	}
}

func (cs *CaptchaSocket) SolveChan() <-chan string {
	return cs.solveCh
}

// handleBlReload re-applies the bypass routes for the running daemon from the
// new bl-file (the -bl value is taken from daemonReloadCtx). It is invoked by
// the BL_RELOAD control command over the daemon's unix socket.
func handleBlReload(newFile string) string {
	summary, err := reloadSplitRoutes(daemonMode, daemonBlRaw, daemonProfileName, newFile)
	if err != nil {
		fmt.Printf("[ERROR] reload bl: %v\n", err)
		return "RELOAD_ERR|" + err.Error()
	}
	fmt.Printf("[OK] %s\n", summary)
	return "RELOAD_OK|" + summary
}

func (cs *CaptchaSocket) Stop() {
	cs.closed = true
	if cs.listener != nil {
		_ = cs.listener.Close()
	}
	_ = os.Remove(cs.path)
}

// Client side: send a CAPTCHA result over the unix socket
func sendCaptchaResult(socketPath, result string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	_, err = fmt.Fprintf(conn, "CAPTCHA_RESULT|%s\n", result)
	if err != nil {
		return fmt.Errorf("failed to send result: %w", err)
	}
	return nil
}

// sendBlReload sends a BL_RELOAD control command to the running daemon over its
// unix socket and returns the daemon's reply line (e.g. "RELOAD_OK|...").
func sendBlReload(sockPath, abspath string) (string, error) {
	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("не удалось подключиться к сокету демона %q: %w", sockPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(conn, "BL_RELOAD|%s\n", abspath); err != nil {
		return "", fmt.Errorf("не удалось отправить команду демону: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("ошибка чтения ответа от демона: %w", err)
	}
	return "", fmt.Errorf("демон не ответил (соединение закрыто)")
}

// ForwardStdinToSocket reads CAPTCHA_RESULT lines from stdin and forwards them
// to the daemon's unix socket. This runs in the parent process after fork.
func forwardStdinToSocket(socketPath string, stopCh <-chan struct{}) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		select {
		case <-stopCh:
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "CAPTCHA_RESULT|") {
			if err := sendCaptchaResult(socketPath, strings.TrimPrefix(line, "CAPTCHA_RESULT|")); err != nil {
				fmt.Fprintf(os.Stderr, "[WARNING] Не удалось отправить результат капчи: %v\n", err)
			}
		}
	}
}

// waitForSocket waits for the unix socket to become available (daemon started)
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not created within %s", path, timeout)
}

func socketDir() string {
	return filepath.Dir(socketPath(""))
}
