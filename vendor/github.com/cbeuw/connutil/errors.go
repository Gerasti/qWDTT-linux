package connutil

import "errors"

var (
	ErrTimeout        = errors.New("deadline exceeded")
	ErrListenerClosed = errors.New("the listener is closed")
	ErrWriteToLarge   = errors.New("write is too large for the buffer")
)
