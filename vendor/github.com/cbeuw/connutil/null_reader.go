package connutil

import "io"

type NullReader struct{}

func (n *NullReader) Read(b []byte) (int, error) {
	l := len(b)
	for i := 0; i < l; i++ {
		b[i] = 0
	}
	return l, nil
}

func (n *NullReader) WriteTo(w io.Writer) (i int64, err error) {
	null := make([]byte, 16*1024)
	var written int
	for err == nil {
		written, err = w.Write(null)
		i += int64(written)
	}
	return
}
