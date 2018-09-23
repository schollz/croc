package comm

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
	if n != len(b) {
		err = fmt.Errorf("wanted to write %d but wrote %d", n, len(b))
	}
	// log.Printf("wanted to write %d but wrote %d", n, len(b))
	return n, err
}

func (c Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	bs = make([]byte, 2)
	_, err = c.connection.Read(bs)
	if err != nil {
		return
	}
	for {
		bs = bytes.Trim(bytes.Trim(bs, "\x00"), "\x05")
		if len(bs) == 2 {
			break
		}
		tmp := make([]byte, 1)
		c.connection.Read(tmp)
		bs = append(bs, tmp...)
	}
	numBytes = int(binary.LittleEndian.Uint16(bs[:]))
	buf = []byte{}
	tmp := make([]byte, numBytes)
	for {
		_, err = c.connection.Read(tmp)
		if err != nil {
			return nil, numBytes, bs, err
		}
		tmp = bytes.Trim(tmp, "\x00")
		tmp = bytes.Trim(tmp, "\x05")
		buf = append(buf, tmp...)
		if len(buf) < numBytes {
			tmp = make([]byte, numBytes-len(buf))
		} else {
			break
		}
	}
	// log.Printf("wanted %d and got %d", numBytes, len(buf))
	return
}

// Send a message
func (c Comm) Send(message string) (err error) {
	_, err = c.Write([]byte(message))
	return
}

// Receive a message
func (c Comm) Receive() (s string, err error) {
	b, _, _, err := c.Read()
	s = string(b)
	return
}
