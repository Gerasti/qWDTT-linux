package connutil

import (
	"io"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Discard returns a net.Conn on which Write calls will succeed without doing anything, until WriteDeadline is reached or
// the Conn is closed.
// Read calls will block until either the ReadDeadline is reached or the Conn is closed
func Discard() net.Conn {
	d := &discardConn{}
	d.rCond.L = &sync.Mutex{}
	return d
}

type discardConn struct {
	closed uint32
	rCond  sync.Cond

	deadlineM sync.RWMutex
	rDeadline time.Time
	wDeadline time.Time
}

func (d *discardConn) Read(b []byte) (int, error) {
	for {
		if atomic.LoadUint32(&d.closed) == 1 {
			return 0, io.ErrClosedPipe
		}
		d.deadlineM.RLock()
		if !d.rDeadline.IsZero() {
			delta := time.Until(d.rDeadline)
			if delta <= 0 {
				d.deadlineM.RUnlock()
				return 0, ErrTimeout
			}
			time.AfterFunc(delta, d.rCond.Broadcast)
		}
		d.deadlineM.RUnlock()

		d.rCond.L.Lock()
		d.rCond.Wait()
		d.rCond.L.Unlock()
	}
}

func (d *discardConn) Write(b []byte) (int, error) {
	if atomic.LoadUint32(&d.closed) == 1 {
		return 0, io.ErrClosedPipe
	}
	d.deadlineM.RLock()
	defer d.deadlineM.RUnlock()
	if !d.wDeadline.IsZero() {
		delta := time.Until(d.wDeadline)
		if delta <= 0 {
			return 0, ErrTimeout
		}
	}
	return ioutil.Discard.Write(b)
}

func (d *discardConn) Close() error {
	atomic.SwapUint32(&d.closed, 1)
	d.rCond.Broadcast()
	return nil
}
func (d *discardConn) SetReadDeadline(t time.Time) error {
	d.deadlineM.Lock()
	defer d.deadlineM.Unlock()

	d.rDeadline = t
	return nil
}
func (d *discardConn) SetWriteDeadline(t time.Time) error {
	d.deadlineM.Lock()
	defer d.deadlineM.Unlock()

	d.wDeadline = t
	return nil
}
func (d *discardConn) SetDeadline(t time.Time) error {
	d.deadlineM.Lock()
	defer d.deadlineM.Unlock()
	d.rDeadline = t
	d.wDeadline = t
	return nil
}
func (d *discardConn) LocalAddr() net.Addr  { return fakeAddr{} }
func (d *discardConn) RemoteAddr() net.Addr { return fakeAddr{} }
