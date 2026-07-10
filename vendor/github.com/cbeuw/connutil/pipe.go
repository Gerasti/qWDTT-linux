package connutil

import (
	"net"
	"time"
)

// PacketPipe represents one end of an asynchronous, stream-oriented pipe.
// A pipe with two connected PacketPipe ends can be created through func AsyncPipe and func LimitedAsyncPipe.
type StreamPipe struct {
	writeEnd *bufferedPipe
	readEnd  *bufferedPipe
}

// Read implements net.Conn Read method. It will block until data becomes available by writing to the other end.
func (conn *StreamPipe) Read(b []byte) (int, error) { return conn.readEnd.Read(b) }

// Write implements net.Conn Read method. If a buffer size is specified using LimitedAsyncPipe, it may block
// until data is read from the other end.
func (conn *StreamPipe) Write(b []byte) (int, error) { return conn.writeEnd.Write(b) }

// Close closes the pipe. Calling Close on either end of a pipe will close both ends.
func (conn *StreamPipe) Close() error {
	conn.writeEnd.Close()
	conn.readEnd.Close()
	return nil
}

// SetReadDeadline implements net.Conn SetReadDeadline method.
func (conn *StreamPipe) SetReadDeadline(t time.Time) error {
	conn.readEnd.SetReadDeadline(t)
	return nil
}

// SetWriteDeadline implements net.Conn SetWriteDeadline method.
func (conn *StreamPipe) SetWriteDeadline(t time.Time) error {
	conn.writeEnd.SetWriteDeadline(t)
	return nil
}

// SetDeadline implements net.Conn SetDeadline method.
func (conn *StreamPipe) SetDeadline(t time.Time) error {
	_ = conn.SetReadDeadline(t)
	_ = conn.SetWriteDeadline(t)
	return nil
}

// LocalAddr implements net.Conn LocalAddr method. It returns a meaningless mock address.
func (conn *StreamPipe) LocalAddr() net.Addr { return fakeAddr{} }

// RemoteAddr implements net.Conn RemoteAddr method. It returns a meaningless mock address.
func (conn *StreamPipe) RemoteAddr() net.Addr { return fakeAddr{} }

// AsyncPipe is an in-memory, full-duplex pipe with both ends implementing net.Conn interface.
//
// It is a drop-in replacement of net.Pipe, but buffered, asynchronous and safe for concurrent use.
func AsyncPipe() (*StreamPipe, *StreamPipe) {
	return LimitedAsyncPipe(0)
}

// LimitedAsyncPipe is similar to AsyncPipe, but Write calls will block if the buffer size grows larger than
// bufferSizeLimit. Consuming data through Read calls on the other end will unblock the Write call.
func LimitedAsyncPipe(bufferSizeLimit int) (*StreamPipe, *StreamPipe) {
	LtoR := newBufferedPipe(bufferSizeLimit)
	RtoL := newBufferedPipe(bufferSizeLimit)
	a := &StreamPipe{
		writeEnd: LtoR,
		readEnd:  RtoL,
	}
	b := &StreamPipe{
		writeEnd: RtoL,
		readEnd:  LtoR,
	}
	return a, b
}
