package comm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
)

// Comm is some basic TCP communication
type Comm struct {
	connection net.Conn
}

// NewConnection gets a new comm to a tcp address
func NewConnection(address string) (c *Comm, err error) {
	connection, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return
	}
	c = New(connection)
	return
}

// New returns a new comm
func New(c net.Conn) *Comm {
	c.SetReadDeadline(time.Now().Add(3 * time.Hour))
	c.SetDeadline(time.Now().Add(3 * time.Hour))
	c.SetWriteDeadline(time.Now().Add(3 * time.Hour))
	comm := new(Comm)
	comm.connection = c
	return comm
}

// Connection returns the net.Conn connection
func (c *Comm) Connection() net.Conn {
	return c.connection
}

// Close closes the connection
func (c *Comm) Close() {
	c.connection.Close()
}

func (c *Comm) Write(b []byte) (int, error) {
	header := new(bytes.Buffer)
	err := binary.Write(header, binary.LittleEndian, uint32(len(b)))
	if err != nil {
		fmt.Println("binary.Write failed:", err)
	}
	tmpCopy := append(header.Bytes(), b...)
	n, err := c.connection.Write(tmpCopy)
	if n != len(tmpCopy) {
		if err != nil {
			err = errors.Wrap(err, fmt.Sprintf("wanted to write %d but wrote %d", len(b), n))
		} else {
			err = fmt.Errorf("wanted to write %d but wrote %d", len(b), n)
		}
	}
	// log.Printf("wanted to write %d but wrote %d", n, len(b))
	return n, err
}

func (c *Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	// read until we get 5 bytes
	header := make([]byte, 4)
	n, err := c.connection.Read(header)
	if err != nil {
		return
	}
	if n < 4 {
		err = fmt.Errorf("not enough bytes: %d", n)
		return
	}
	// make it so it won't change
	header = append([]byte(nil), header...)

	var numBytesUint32 uint32
	rbuf := bytes.NewReader(header)
	err = binary.Read(rbuf, binary.LittleEndian, &numBytesUint32)
	if err != nil {
		fmt.Println("binary.Read failed:", err)
	}
	numBytes = int(numBytesUint32)
	for {
		tmp := make([]byte, numBytes)
		n, errRead := c.connection.Read(tmp)
		if errRead != nil {
			err = errRead
			return
		}
		buf = append(buf, tmp[:n]...)
		if numBytes == len(buf) {
			break
		}
	}
	return
}

// Send a message
func (c *Comm) Send(message []byte) (err error) {
	_, err = c.Write(message)
	return
}

// Receive a message
func (c *Comm) Receive() (b []byte, err error) {
	b, _, _, err = c.Read()
	return
}
