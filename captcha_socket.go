package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CaptchaSocket struct {
	path     string
	listener net.Listener
	solveCh  chan string
	closed   bool
}

func newCaptchaSocket(profile string) *CaptchaSocket {
	return &CaptchaSocket{
		path:    socketPath(profile),
		solveCh: make(chan string, 1),
	}
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
	}
}

func (cs *CaptchaSocket) SolveChan() <-chan string {
	return cs.solveCh
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
