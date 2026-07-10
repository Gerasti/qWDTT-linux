package connutil

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// softLimit == 0 means no limit
func newBufferedPacketPipe(softLimit int) *bufferedPacketPipe {
	p := &bufferedPacketPipe{softLimit: softLimit}
	p.rCond.L = &p.mu
	p.wCond.L = &p.mu
	return p
}

type bufferedPacketPipe struct {
	softLimit int
	mu        sync.Mutex
	pLens     []int
	buf       bytes.Buffer
	closed    bool
	rCond     sync.Cond
	wCond     sync.Cond
	rDeadline time.Time
	wDeadline time.Time
}

func (p *bufferedPacketPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for {
		if !p.rDeadline.IsZero() {
			d := time.Until(p.rDeadline)
			if d <= 0 {
				return 0, ErrTimeout
			}
			time.AfterFunc(d, p.rCond.Broadcast)
		}
		if len(p.pLens) > 0 {
			break
		}
		if p.closed {
			return 0, io.ErrClosedPipe
		}
		p.rCond.Wait()
	}

	curLen := p.pLens[0]
	if curLen > len(b) {
		return 0, io.ErrShortBuffer
	}
	n, _ := p.buf.Read(b[:curLen])
	p.pLens = p.pLens[1:]
	p.wCond.Broadcast()
	if p.closed {
		return n, io.ErrClosedPipe
	}
	// err is either io.EOF or nil. Since the buffer is definitely not empty, err is nil
	return n, nil
}

func (p *bufferedPacketPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.softLimit != 0 && len(b) > p.softLimit {
		return 0, ErrWriteToLarge
	}

	for {
		if p.closed {
			return 0, io.ErrClosedPipe
		}
		if !p.wDeadline.IsZero() {
			d := time.Until(p.wDeadline)
			if d <= 0 {
				return 0, ErrTimeout
			}
			time.AfterFunc(d, p.wCond.Broadcast)
		}
		if p.softLimit == 0 {
			break
		} else {
			if p.buf.Len() <= p.softLimit {
				break
			}
			p.wCond.Wait()
		}
	}

	p.pLens = append(p.pLens, len(b))
	p.buf.Write(b)
	// err is always nil
	p.rCond.Broadcast()
	return len(b), nil
}

func (p *bufferedPacketPipe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	p.rCond.Broadcast()
	p.wCond.Broadcast()
}

func (p *bufferedPacketPipe) SetReadDeadline(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.rDeadline = t
	p.rCond.Broadcast()
}

func (p *bufferedPacketPipe) SetWriteDeadline(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.wDeadline = t
	p.wCond.Broadcast()
}
