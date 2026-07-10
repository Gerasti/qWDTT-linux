package connutil

import (
	"net"
	"time"
)

// PacketPipe represents one end of an asynchronous, packet-oriented pipe.
// A pipe with two connected PacketPipe ends can be created through func AsyncPacketPipe and func LimitedAsyncPacketPipe.
type PacketPipe struct {
	writeEnd *bufferedPacketPipe
	readEnd  *bufferedPacketPipe
}

// ReadFrom implements the net.PacketConn ReadFrom method. It behaves in the same way as Read.
// The returned addr is not null but serves no purpose.
func (conn *PacketPipe) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, err = conn.Read(p)
	addr = conn.RemoteAddr()
	return
}

// WriteTo implements the net.PacketConn WriteTo method. It behaves in the same way as Write.
// The addr argument is discarded.
func (conn *PacketPipe) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	n, err = conn.Write(p)
	return
}

// Read reads a packet from the pipe into p. Read calls will block until data becomes available by writing to the other end.
// If the len(p) is smaller than the size of the packet, nothing will be read and err will be io.ShortBuffer.
func (conn *PacketPipe) Read(p []byte) (n int, err error) {
	n, err = conn.readEnd.Read(p)
	return n, err
}

// Write writes a packet from p to the pipe. If a buffer size is specified using LimitedAsyncPacketPipe, it may block
// until data is read from the other end.
// If len(p) is larger than the buffer size, err will be ErrWriteToLarge.
func (conn *PacketPipe) Write(p []byte) (n int, err error) {
	n, err = conn.writeEnd.Write(p)
	return
}

// Close closes the pipe. Calling Close on either end of a pipe will close both ends.
func (conn *PacketPipe) Close() error {
	conn.writeEnd.Close()
	conn.readEnd.Close()
	return nil
}

// SetReadDeadline implements net.Conn and net.PacketConn SetReadDeadline method.
func (conn *PacketPipe) SetReadDeadline(t time.Time) error {
	conn.readEnd.SetReadDeadline(t)
	return nil
}

// SetWriteDeadline implements net.Conn and net.PacketConn SetWriteDeadline method.
func (conn *PacketPipe) SetWriteDeadline(t time.Time) error {
	conn.writeEnd.SetWriteDeadline(t)
	return nil
}

// SetDeadline implements net.Conn and net.PacketConn SetDeadline method.
func (conn *PacketPipe) SetDeadline(t time.Time) error {
	_ = conn.SetReadDeadline(t)
	_ = conn.SetWriteDeadline(t)
	return nil
}

// LocalAddr implements net.Conn and net.PacketConn LocalAddr method. It returns a meaningless mock address.
func (conn *PacketPipe) LocalAddr() net.Addr { return fakeAddr{} }

// RemoteAddr implements net.Conn and net.PacketConn RemoteAddr method. It returns a meaningless mock address.
func (conn *PacketPipe) RemoteAddr() net.Addr { return fakeAddr{} }

// AsyncPipe creates an in-memory, full-duplex, packet-oriented pipe with both ends implementing net.Conn and net.PacketConn
// interfaces. It is a drop-in replacement of net.Pipe, but creates a packet-oriented pipe instead.
//
// It is buffered, asynchronous and safe for concurrent use.
func AsyncPacketPipe() (*PacketPipe, *PacketPipe) {
	return LimitedAsyncPacketPipe(0)
}

// LimitedAsyncPipe is similar to AsyncPipe, but limits the size of the underlying buffer.
// Write calls will block if the buffer size grows larger than
// bufferSizeLimit. Consuming data through Read calls on the other end will unblock the Write call.
func LimitedAsyncPacketPipe(bufferSizeLimit int) (*PacketPipe, *PacketPipe) {
	LtoR := newBufferedPacketPipe(bufferSizeLimit)
	RtoL := newBufferedPacketPipe(bufferSizeLimit)
	a := &PacketPipe{
		writeEnd: LtoR,
		readEnd:  RtoL,
	}
	b := &PacketPipe{
		writeEnd: RtoL,
		readEnd:  LtoR,
	}
	return a, b
}
