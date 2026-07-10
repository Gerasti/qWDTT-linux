package connutil

import (
	"io"
	"net"
)

func echo(conn net.Conn) {
	_, err := io.Copy(conn, conn)
	if err != nil {
		conn.Close()
	}
}

// Echoer returns a net.Conn to which everything written will be echoed back
func Echoer() net.Conn {
	a, b := AsyncPipe()
	go echo(b)
	return a
}
