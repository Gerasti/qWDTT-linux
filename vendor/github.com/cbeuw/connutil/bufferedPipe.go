/*
Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

/*
Forked from https://github.com/golang/go/blob/74d6de03fd7db2c6faa7794620a9bcf0c4f018f2/src/net/net_fake.go#L166
*/

package connutil

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// softLimit == 0 means no limit
func newBufferedPipe(softLimit int) *bufferedPipe {
	p := &bufferedPipe{softLimit: softLimit}
	p.rCond.L = &p.mu
	p.wCond.L = &p.mu
	return p
}

type bufferedPipe struct {
	softLimit int
	mu        sync.Mutex
	buf       bytes.Buffer
	closed    bool
	rCond     sync.Cond
	wCond     sync.Cond
	rDeadline time.Time
	wDeadline time.Time
}

func (p *bufferedPipe) Read(b []byte) (int, error) {
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
		if p.closed || p.buf.Len() > 0 {
			break
		}
		p.rCond.Wait()
	}

	n, _ := p.buf.Read(b)
	p.wCond.Broadcast()
	if p.closed {
		return n, io.ErrClosedPipe
	}
	// err is either io.EOF or nil. Since the buffer is definitely not empty, err is nil
	return n, nil
}

func (p *bufferedPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

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

	p.buf.Write(b)
	// err is always nil
	p.rCond.Broadcast()
	return len(b), nil
}

func (p *bufferedPipe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	p.rCond.Broadcast()
	p.wCond.Broadcast()
}

func (p *bufferedPipe) SetReadDeadline(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.rDeadline = t
	p.rCond.Broadcast()
}

func (p *bufferedPipe) SetWriteDeadline(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.wDeadline = t
	p.wCond.Broadcast()
}
