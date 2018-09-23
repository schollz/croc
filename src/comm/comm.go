package comm

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"
)

// Comm is some basic TCP communication
type Comm struct {
	connection net.Conn
}

// New returns a new comm
func New(c net.Conn) Comm {
	c.SetReadDeadline(time.Now().Add(3 * time.Hour))
	c.SetDeadline(time.Now().Add(3 * time.Hour))
	c.SetWriteDeadline(time.Now().Add(3 * time.Hour))
	return Comm{c}
}

// Connection returns the net.Conn connection
func (c Comm) Connection() net.Conn {
	return c.connection
}

func (c Comm) Write(b []byte) (int, error) {
	bs := make([]byte, 2)
	binary.LittleEndian.PutUint16(bs, uint16(len(b)))
	c.connection.Write(bs)
	n, err := c.connection.Write(b)
	return n, err
}

func (c Comm) Read() (buf []byte, err error) {
	bs := make([]byte, 2)
	_, err = c.connection.Read(bs)
	if err != nil {
		return
	}
	numBytes := int(binary.LittleEndian.Uint16(bs[:2]))
	buf = []byte{}
	tmp := make([]byte, numBytes)
	for {
		n, err := c.connection.Read(tmp)
		if err != nil {
			return nil, err
		}
		tmp = bytes.TrimRight(tmp, "\x00")
		tmp = bytes.TrimLeft(tmp, "\x00")
		tmp = bytes.TrimRight(tmp, "\x05")
		tmp = bytes.TrimLeft(tmp, "\x05")
		buf = append(buf, tmp...)
		if n < numBytes {
			numBytes -= n
			tmp = make([]byte, numBytes)
		} else {
			break
		}
	}
	return
}

// Send a message
func (c Comm) Send(message string) (err error) {
	_, err = c.Write([]byte(message))
	return
}

// Receive a message
func (c Comm) Receive() (s string, err error) {
	b, err := c.Read()
	s = string(b)
	return
}
