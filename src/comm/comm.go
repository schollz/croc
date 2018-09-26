package comm

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Comm is some basic TCP communication
type Comm struct {
	connection *net.TCPConn
	writer     *bufio.Writer
}

// New returns a new comm
func New(n *net.TCPConn) *Comm {
	c := new(Comm)
	c.connection = n
	c.connection.SetReadDeadline(time.Now().Add(3 * time.Hour))
	c.connection.SetDeadline(time.Now().Add(3 * time.Hour))
	c.connection.SetWriteDeadline(time.Now().Add(3 * time.Hour))
	return c
}

// Connection returns the net.TCPConn connection
func (c *Comm) Connection() *net.TCPConn {
	return c.connection
}

// Close closes the connection
func (c *Comm) Close() {
	c.connection.Close()
}

func (c *Comm) Write(b []byte) (int, error) {
	c.connection.Write([]byte(fmt.Sprintf("%0.6d", len(b))))
	n, err := c.connection.Write(b)
	if n != len(b) {
		err = fmt.Errorf("wanted to write %d but wrote %d", n, len(b))
	}
	// if err == nil {
	// 	c.connection.Flush()
	// }
	// log.Printf("wanted to write %d but wrote %d", n, len(b))
	return n, err
}

// func (c *Comm) Flush() {
// 	c.connection.Flush()
// }

func (c *Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	// read until we get 6 bytes
	tmp := make([]byte, 6)
	n, err := c.connection.Read(tmp)
	if err != nil {
		return
	}
	tmpCopy := make([]byte, n)
	// Copy the buffer so it doesn't get changed while read by the recipient.
	copy(tmpCopy, tmp[:n])
	bs = tmpCopy

	tmp = make([]byte, 1)
	for {
		// see if we have enough bytes
		bs = bytes.Trim(bs, "\x00")
		if len(bs) == 6 {
			break
		}
		n, err := c.connection.Read(tmp)
		if err != nil {
			return nil, 0, nil, err
		}
		tmpCopy = make([]byte, n)
		// Copy the buffer so it doesn't get changed while read by the recipient.
		copy(tmpCopy, tmp[:n])
		bs = append(bs, tmpCopy...)
	}

	numBytes, err = strconv.Atoi(strings.TrimLeft(string(bs), "0"))
	if err != nil {
		return nil, 0, nil, err
	}
	buf = []byte{}
	tmp = make([]byte, numBytes)
	for {
		n, err := c.connection.Read(tmp)
		if err != nil {
			return nil, 0, nil, err
		}
		tmpCopy = make([]byte, n)
		// Copy the buffer so it doesn't get changed while read by the recipient.
		copy(tmpCopy, tmp[:n])
		buf = append(buf, bytes.TrimRight(tmpCopy, "\x00")...)
		if len(buf) < numBytes {
			// shrink the amount we need to read
			tmp = tmp[:numBytes-len(buf)]
		} else {
			break
		}
	}
	// log.Printf("wanted %d and got %d", numBytes, len(buf))
	return
}

// Send a message
func (c *Comm) Send(message string) (err error) {
	_, err = c.Write([]byte(message))
	return
}

// Receive a message
func (c *Comm) Receive() (s string, err error) {
	b, _, _, err := c.Read()
	s = string(b)
	return
}
