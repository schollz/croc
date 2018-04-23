package main

import (
	"encoding/hex"
	"io"
)

const bufsize = 256

type hexdump struct {
	w   io.Writer
	buf [bufsize]byte
}

func hexoutput(w io.Writer) io.WriteCloser {
	return &hexdump{w: w}
}

func (h *hexdump) Write(data []byte) (n int, err error) {
	for n < len(data) {
		amt := len(data) - n
		if hex.EncodedLen(amt) > bufsize {
			amt = hex.DecodedLen(bufsize)
		}
		nn := hex.Encode(h.buf[:], data[n:n+amt])
		_, err := h.w.Write(h.buf[:nn])
		n += amt
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (h *hexdump) Close() error {
	_, err := h.w.Write([]byte{'\n'})
	return err
}
