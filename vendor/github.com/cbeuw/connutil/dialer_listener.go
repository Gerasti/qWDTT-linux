package connutil

import (
	"context"
	"net"
	"sync/atomic"
)

// Dialer is an interface such that the net.Dialer type implements this. This interface type can be used
// as a drop-in replacement for type bindings where methods of net.Dialer are called.
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// PipeDialer implements the Dialer interface, which means it has the same method signatures as net.Dialer
type PipeDialer struct {
	// PipeBufferSize specifies the limit on the underlying buffer size. Default (0) means unlimited.
	BufferSizeLimit int
	peer            *PipeListener
}

// Dial returns one end of the pipe.
// A *StreamPipe (implementing net.Conn) is returned when the network argument is empty, or "tcp", "tcp4" or "tcp6";
// a *PacketPipe (implementing both net.Conn and net.PacketConn) is returned when the network argument is "udp", "udp4",
// "udp6", "ip", "ip4", "ip6", "unix", "unixgram" or "unixpacket".
//
// address argument doesn't do anything
func (d *PipeDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

// DialContext acts like Dial but it may timeout or be cancelled using ctx.
func (d *PipeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if atomic.LoadUint32(&d.peer.closed) == 1 {
		return nil, ErrListenerClosed
	}

	switch network {
	case "udp", "udp4", "udp6", "ip", "ip4", "ip6", "unix", "unixgram", "unixpacket":
		a, b := LimitedAsyncPacketPipe(d.BufferSizeLimit)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case d.peer.incomingPacketConn <- b:
			return a, nil
		}
	default:
		a, b := LimitedAsyncPipe(d.BufferSizeLimit)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case d.peer.incomingStreamConn <- b:
			return a, nil
		}
	}
}

// PipeListener is a net.Listener that accepts connections dialed from the corresponding PipeDialer
type PipeListener struct {
	incomingStreamConn chan net.Conn
	incomingPacketConn chan net.PacketConn
	closed             uint32
}

// Accept implements Listener.Accept(). It returns one end of a StreamPipe, with the other end obtained through the
// corresponding PipeDialer.Dial with an empty or stream-oriented network argument.
func (l *PipeListener) Accept() (net.Conn, error) {
	if atomic.LoadUint32(&l.closed) == 1 {
		return nil, ErrListenerClosed
	} else {
		return <-l.incomingStreamConn, nil
	}
}

// Close implements Listener.Close()
func (l *PipeListener) Close() error {
	atomic.StoreUint32(&l.closed, 1)
	return nil
}

// Addr returns a fake address
func (l *PipeListener) Addr() net.Addr {
	return fakeAddr{}
}

// ListenPacket has the same function signature as net.ListenPacket function, meaning it's a drop-in replacement.
// It returns one end of a PacketPipe, with the other end through the corresponding PipeDialer.Dial using a
// packet-oriented network argument.
//
// network and address arguments don't do anything
func (l *PipeListener) ListenPacket(network, address string) (net.PacketConn, error) {
	if atomic.LoadUint32(&l.closed) == 1 {
		return nil, ErrListenerClosed
	} else {
		return <-l.incomingPacketConn, nil
	}
}

// DialerListener returns a pair of PipeDialer and PipeListener.
// To obtain a stream-oriented pipe, call PipeDialer.Dial with an empty or stream-oriented network argument and then
// call PipeListener.Accept.
//
// Similarly, to obtain a packet-oriented pipe, call PipeDialer.Dial with a packet-oriented network argument (e.g. "udp")
// and then call PipeListener.ListenPacket, as if calling ListenPacket function from the net package.
//
// backlog specifies the amount of Dial calls you can make without making corresponding Accept or ListenPacket calls on
// listener before Dial calls start blocking.
func DialerListener(backlog int) (*PipeDialer, *PipeListener) {
	l := &PipeListener{
		incomingStreamConn: make(chan net.Conn, backlog),
		incomingPacketConn: make(chan net.PacketConn, backlog),
		closed:             0,
	}
	d := &PipeDialer{peer: l}
	return d, l
}
