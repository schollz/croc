package comm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	log "github.com/schollz/logger"
)

const MAXBYTES = 4000000

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
		err = fmt.Errorf("comm.NewConnection failed: %w", err)
		return
	}
	c = New(connection)
	log.Debugf("connected to '%s'", address)
	return
}

// New returns a new comm
func New(c net.Conn) *Comm {
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Warnf("error setting read deadline: %v", err)
	}
	if err := c.SetDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Warnf("error setting overall deadline: %v", err)
	}
	if err := c.SetWriteDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Errorf("error setting write deadline: %v", err)
	}
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
	if err := c.connection.Close(); err != nil {
		log.Warnf("error closing connection: %v", err)
	}
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
			err = fmt.Errorf("wanted to write %d but wrote %d: %w", len(b), n, err)
		} else {
			err = fmt.Errorf("wanted to write %d but wrote %d", len(b), n)
		}
	}
	// log.Printf("wanted to write %d but wrote %d", n, len(b))
	return n, err
}

func (c *Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	// long read deadline in case waiting for file
	if err := c.connection.SetReadDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Warnf("error setting read deadline: %v", err)
	}

	// read until we get 4 bytes for the header
	var header []byte
	numBytes = 4
	for {
		tmp := make([]byte, numBytes-len(header))
		n, errRead := c.connection.Read(tmp)
		if errRead != nil {
			err = errRead
			log.Debugf("initial read error: %v", err)
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
		err = fmt.Errorf("binary.Read failed: %w", err)
		log.Debug(err.Error())
		return
	}
	numBytes = int(numBytesUint32)
	if numBytes > MAXBYTES {
		err = fmt.Errorf("too many bytes: %d", numBytes)
		log.Debug(err)
		return
	}
	buf = make([]byte, 0)

	// shorten the reading deadline in case getting weird data
	if err := c.connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		log.Warnf("error setting read deadline: %v", err)
	}
	for {
		// log.Debugf("bytes: %d/%d", len(buf), numBytes)
		tmp := make([]byte, numBytes-len(buf))
		n, errRead := c.connection.Read(tmp)
		if errRead != nil {
			err = errRead
			log.Debugf("consecutive read error: %v", err)
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
