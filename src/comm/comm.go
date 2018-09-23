package comm

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
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
	c.connection.Write([]byte(fmt.Sprintf("%0.5d", len(b))))
	n, err := c.connection.Write(b)
	if n != len(b) {
		err = fmt.Errorf("wanted to write %d but wrote %d", n, len(b))
	}
	// log.Printf("wanted to write %d but wrote %d", n, len(b))
	return n, err
}

func (c Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	// read until we get 5 bytes
	bs = make([]byte, 5)
	_, err = c.connection.Read(bs)
	if err != nil {
		return
	}
	for {
		bs = bytes.Trim(bytes.Trim(bs, "\x00"), "\x05")
		if len(bs) == 5 {
			break
		}
		tmp := make([]byte, 1)
		c.connection.Read(tmp)
		bs = append(bs, tmp...)
		log.Println(bs)
	}
	numBytes, err = strconv.Atoi(strings.TrimLeft(string(bs), "0"))
	if err != nil {
		return nil, 0, nil, err
	}
	buf = []byte{}
	tmp := make([]byte, numBytes)
	for {
		_, err = c.connection.Read(tmp)
		if err != nil {
			return nil, numBytes, bs, err
		}
		tmp = bytes.TrimRight(tmp, "\x00")
		tmp = bytes.TrimRight(tmp, "\x05")
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
