package directquic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	MaxFrameSize     = 64 * 1024
	streamHeaderSize = 8 + 32 + 4 + 2 + 2
)

var streamMagic = [8]byte{'c', 'r', 'o', 'c', 'q', 'v', '1', 0}

type StreamHeader struct {
	SessionID []byte
	FileIndex uint32
	Lane      uint16
	LaneCount uint16
}

func WriteStreamHeader(w io.Writer, header StreamHeader) error {
	if len(header.SessionID) != 32 {
		return fmt.Errorf("invalid stream session ID length %d", len(header.SessionID))
	}
	if header.LaneCount == 0 || header.LaneCount > MaxLanes || header.Lane >= header.LaneCount {
		return errors.New("invalid direct QUIC stream lane")
	}
	data := make([]byte, streamHeaderSize)
	copy(data[:8], streamMagic[:])
	copy(data[8:40], header.SessionID)
	binary.LittleEndian.PutUint32(data[40:44], header.FileIndex)
	binary.LittleEndian.PutUint16(data[44:46], header.Lane)
	binary.LittleEndian.PutUint16(data[46:48], header.LaneCount)
	return writeFull(w, data)
}

func ReadStreamHeader(r io.Reader) (StreamHeader, error) {
	data := make([]byte, streamHeaderSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return StreamHeader{}, err
	}
	if !bytes.Equal(data[:8], streamMagic[:]) {
		return StreamHeader{}, errors.New("invalid direct QUIC stream magic")
	}
	header := StreamHeader{
		SessionID: append([]byte(nil), data[8:40]...),
		FileIndex: binary.LittleEndian.Uint32(data[40:44]),
		Lane:      binary.LittleEndian.Uint16(data[44:46]),
		LaneCount: binary.LittleEndian.Uint16(data[46:48]),
	}
	if header.LaneCount == 0 || header.LaneCount > MaxLanes || header.Lane >= header.LaneCount {
		return StreamHeader{}, errors.New("invalid direct QUIC stream lane")
	}
	return header, nil
}

func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxFrameSize {
		return fmt.Errorf("invalid direct QUIC frame size %d", len(payload))
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeFull(w, header[:]); err != nil {
		return err
	}
	return writeFull(w, payload)
}

func ReadFrame(r io.Reader, reuse []byte) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(header[:]))
	if size <= 0 || size > MaxFrameSize {
		return nil, fmt.Errorf("invalid direct QUIC frame size %d", size)
	}
	if cap(reuse) < size {
		reuse = make([]byte, size)
	} else {
		reuse = reuse[:size]
	}
	_, err := io.ReadFull(r, reuse)
	return reuse, err
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
