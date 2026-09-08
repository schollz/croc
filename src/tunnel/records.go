package tunnel

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	RecordMetadata      byte = 1
	RecordData          byte = 2
	RecordEnd           byte = 3
	RecordError         byte = 4
	RecordText          byte = 5
	RecordBinary        byte = 6
	RecordClose         byte = 7
	MaxRecord                = 64 << 10
	MaxWebSocketMessage      = 1 << 20
	MaxBody                  = 64 << 20
)

// Records bound allocations and preserve WebSocket message boundaries. SSH
// provides encryption, authentication, ordering and per-channel backpressure.
func WriteRecord(w io.Writer, kind byte, data []byte) error {
	limit := MaxRecord
	if kind == RecordText || kind == RecordBinary {
		limit = MaxWebSocketMessage
	}
	if len(data) > limit {
		return errors.New("tunnel record exceeds limit")
	}
	var header [5]byte
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	if n, err := w.Write(header[:]); err != nil {
		return err
	} else if n != len(header) {
		return io.ErrShortWrite
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}
func ReadRecord(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	limit := uint32(MaxRecord)
	if header[0] == RecordText || header[0] == RecordBinary {
		limit = MaxWebSocketMessage
	}
	if size > limit || header[0] < RecordMetadata || header[0] > RecordClose {
		return 0, nil, errors.New("invalid tunnel record")
	}
	body := make([]byte, int(size))
	_, err := io.ReadFull(r, body)
	return header[0], body, err
}
