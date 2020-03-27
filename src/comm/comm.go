package comm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/schollz/logger"
)

const MAXBYTES = 1000000

// Comm is some basic TCP communication
type Comm struct {
	connection net.Conn
}

// NewConnection gets a new comm to a tcp address
func NewConnection(address string, timelimit ...time.Duration) (c *Comm, err error) {
	tlimit := 30 * time.Second
	if len(timelimit) > 0 {
		tlimit = timelimit[0]
	}
	connection, err := net.DialTimeout("tcp", address, tlimit)
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
	// read until we get 4 bytes for the header
	var header []byte
	numBytes = 4
	for {
		tmp := make([]byte, numBytes-len(header))
		n, errRead := c.connection.Read(tmp)
		if errRead != nil {
			err = errRead
			return
		}
		header = append(header, tmp[:n]...)
		if numBytes == len(header) {
			break
		}
	}

	var numBytesUint32 uint32
	rbuf := bytes.NewReader(header)
	err = binary.Read(rbuf, binary.LittleEndian, &numBytesUint32)
	if err != nil {
		err = fmt.Errorf("binary.Read failed: %s", err.Error())
		return
	}
	numBytes = int(numBytesUint32)
	if numBytes > MAXBYTES {
		err = fmt.Errorf("too many bytes: %d", numBytes)
		logger.Error(err)
		return
	}
	buf = make([]byte, 0)
	for {
		// log.Debugf("bytes: %d/%d", len(buf), numBytes)
		tmp := make([]byte, numBytes-len(buf))
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
