package comm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/schollz/croc/v9/src/utils"
	log "github.com/schollz/logger"
	"golang.org/x/net/proxy"
)

var Socks5Proxy = ""

var MAGIC_BYTES = []byte("croc")

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
	var connection net.Conn
	if Socks5Proxy != "" && !utils.IsLocalIP(address) {
		var dialer proxy.Dialer
		// prepend schema if no schema is given
		if !strings.Contains(Socks5Proxy, `://`) {
			Socks5Proxy = `socks5://` + Socks5Proxy
		}
		socks5ProxyURL, urlParseError := url.Parse(Socks5Proxy)
		if urlParseError != nil {
			err = fmt.Errorf("Unable to parse socks proxy url: %s", urlParseError)
			log.Debug(err)
			return
		}
		dialer, err = proxy.FromURL(socks5ProxyURL, proxy.Direct)
		if err != nil {
			err = fmt.Errorf("proxy failed: %w", err)
			log.Debug(err)
			return
		}
		log.Debug("dialing with dialer.Dial")
		connection, err = dialer.Dial("tcp", address)
	} else {
		log.Debugf("dialing to %s with timelimit %s", address, tlimit)
		connection, err = net.DialTimeout("tcp", address, tlimit)
	}
	if err != nil {
		err = fmt.Errorf("comm.NewConnection failed: %w", err)
		log.Debug(err)
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

func (c *Comm) Write(b []byte) (n int, err error) {
	header := new(bytes.Buffer)
	err = binary.Write(header, binary.LittleEndian, uint32(len(b)))
	if err != nil {
		fmt.Println("binary.Write failed:", err)
	}
	tmpCopy := append(header.Bytes(), b...)
	tmpCopy = append(MAGIC_BYTES, tmpCopy...)
	n, err = c.connection.Write(tmpCopy)
	if err != nil {
		err = fmt.Errorf("connection.Write failed: %w", err)
		return
	}
	if n != len(tmpCopy) {
		err = fmt.Errorf("wanted to write %d but wrote %d", len(b), n)
		return
	}
	return
}

func (c *Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	// long read deadline in case waiting for file
	if err := c.connection.SetReadDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Warnf("error setting read deadline: %v", err)
	}
	// must clear the timeout setting
	defer c.connection.SetDeadline(time.Time{})

	// read until we get 4 bytes for the magic
	header := make([]byte, 4)
	_, err = io.ReadFull(c.connection, header)
	if err != nil {
		log.Debugf("initial read error: %v", err)
		return
	}
	if !bytes.Equal(header, MAGIC_BYTES) {
		err = fmt.Errorf("initial bytes are not magic: %x", header)
		return
	}

	// read until we get 4 bytes for the header
	header = make([]byte, 4)
	_, err = io.ReadFull(c.connection, header)
	if err != nil {
		log.Debugf("initial read error: %v", err)
		return
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

	// shorten the reading deadline in case getting weird data
	if err := c.connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		log.Warnf("error setting read deadline: %v", err)
	}
	buf = make([]byte, numBytes)
	_, err = io.ReadFull(c.connection, buf)
	if err != nil {
		log.Debugf("consecutive read error: %v", err)
		return
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
