package connutil

import (
	"io"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Babel returns a net.Conn on which Write calls will succeed without doing anything, until WriteDeadline is reached or
// the Conn is closed.
// Read calls is equivalent to source.Read, until either the ReadDeadline is reached or the Conn is closed
//
// Do Babel(rand.Reader) to get a net.Conn that reads random data
func Babel(source io.Reader) net.Conn {
	return &babelConn{Source: source}
}

type babelConn struct {
	closed uint32
	Source io.Reader

	deadlineM sync.RWMutex
	rDeadline time.Time
	wDeadline time.Time
}

func (b *babelConn) Read(buf []byte) (int, error) {
	if atomic.LoadUint32(&b.closed) == 1 {
		return 0, io.ErrClosedPipe
	}
	b.deadlineM.RLock()
	defer b.deadlineM.RUnlock()
	if !b.rDeadline.IsZero() {
		delta := time.Until(b.rDeadline)
		if delta <= 0 {
			return 0, ErrTimeout
		}
	}

	return b.Source.Read(buf)
}

func (b *babelConn) Write(buf []byte) (int, error) {
	if atomic.LoadUint32(&b.closed) == 1 {
		return 0, io.ErrClosedPipe
	}
	b.deadlineM.RLock()
	defer b.deadlineM.RUnlock()
	if !b.wDeadline.IsZero() {
		delta := time.Until(b.wDeadline)
		if delta <= 0 {
			return 0, ErrTimeout
		}
	}
	return ioutil.Discard.Write(buf)
}

func (b *babelConn) Close() error {
	atomic.SwapUint32(&b.closed, 1)
	return nil
}
func (b *babelConn) SetReadDeadline(t time.Time) error {
	b.deadlineM.Lock()
	defer b.deadlineM.Unlock()

	b.rDeadline = t
	return nil
}
func (b *babelConn) SetWriteDeadline(t time.Time) error {
	b.deadlineM.Lock()
	defer b.deadlineM.Unlock()

	b.wDeadline = t
	return nil
}
func (b *babelConn) SetDeadline(t time.Time) error {
	b.deadlineM.Lock()
	defer b.deadlineM.Unlock()
	b.rDeadline = t
	b.wDeadline = t
	return nil
}
func (b *babelConn) LocalAddr() net.Addr  { return fakeAddr{} }
func (b *babelConn) RemoteAddr() net.Addr { return fakeAddr{} }
