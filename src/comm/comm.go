package comm

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/magisterquis/connectproxy"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/utils"
	"golang.org/x/net/proxy"
)

var Socks5Proxy = ""
var HttpProxy = ""

var MAGIC_BYTES = []byte("croc")

const maxReadMessageSize = 64 * 1024 * 1024

// ErrMessageTooLarge indicates that a framed message exceeds the allocation
// limit accepted by croc peers.
var ErrMessageTooLarge = errors.New("message too large")

// Large resume requests can contain hundreds of thousands of missing chunk
// ranges. Keep the guard against malformed streams, but give legitimate large
// control messages enough time to arrive through a relay.
const messageBodyReadTimeout = 10 * time.Minute

// Comm is some basic TCP communication
type Comm struct {
	connection net.Conn
	writeMu    sync.Mutex
}

// NewConnection gets a new comm to a tcp address
func NewConnection(address string, timelimit ...time.Duration) (c *Comm, err error) {
	return NewConnectionContext(context.Background(), address, timelimit...)
}

// NewConnectionContext gets a new comm to a TCP address and makes direct and
// proxy dials return promptly when ctx is canceled.
func NewConnectionContext(ctx context.Context, address string, timelimit ...time.Duration) (c *Comm, err error) {
	if ctx == nil {
		return nil, errors.New("comm connection context is required")
	}
	tlimit := 30 * time.Second
	if len(timelimit) > 0 {
		tlimit = timelimit[0]
	}
	dialCtx := ctx
	cancel := func() {}
	if tlimit > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, tlimit)
	}
	defer cancel()
	var connection net.Conn
	if Socks5Proxy != "" && !utils.IsLocalIP(address) {
		var dialer proxy.Dialer
		proxyAddress := Socks5Proxy
		// prepend schema if no schema is given
		if !strings.Contains(proxyAddress, `://`) {
			proxyAddress = `socks5://` + proxyAddress
		}
		socks5ProxyURL, urlParseError := url.Parse(proxyAddress)
		if urlParseError != nil {
			err = fmt.Errorf("unable to parse socks proxy url: %s", urlParseError)
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
		connection, err = dialProxyContext(dialCtx, dialer, "tcp", address)
	} else if HttpProxy != "" && !utils.IsLocalIP(address) {
		var dialer proxy.Dialer
		proxyAddress := HttpProxy
		// prepend schema if no schema is given
		if !strings.Contains(proxyAddress, `://`) {
			proxyAddress = `http://` + proxyAddress
		}
		HttpProxyURL, urlParseError := url.Parse(proxyAddress)
		if urlParseError != nil {
			err = fmt.Errorf("unable to parse http proxy url: %s", urlParseError)
			log.Debug(err)
			return
		}
		dialer, err = connectproxy.New(HttpProxyURL, proxy.Direct)
		if err != nil {
			err = fmt.Errorf("proxy failed: %w", err)
			log.Debug(err)
			return
		}
		log.Debug("dialing with dialer.Dial")
		connection, err = dialProxyContext(dialCtx, dialer, "tcp", address)

	} else {
		log.Debugf("dialing to %s with timelimit %s", address, tlimit)
		connection, err = (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
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

type dialResult struct {
	connection net.Conn
	err        error
}

func dialProxyContext(ctx context.Context, dialer proxy.Dialer, network, address string) (net.Conn, error) {
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		connection, err := contextDialer.DialContext(ctx, network, address)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if connection != nil {
				_ = connection.Close()
			}
			return nil, ctxErr
		}
		return connection, err
	}
	result := make(chan dialResult)
	go func() {
		connection, err := dialer.Dial(network, address)
		select {
		case result <- dialResult{connection: connection, err: err}:
		case <-ctx.Done():
			if connection != nil {
				_ = connection.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case dialed := <-result:
		if err := ctx.Err(); err != nil {
			if dialed.connection != nil {
				_ = dialed.connection.Close()
			}
			return nil, err
		}
		return dialed.connection, dialed.err
	}
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
		if errors.Is(err, net.ErrClosed) {
			return
		}
		log.Warnf("error closing connection: %v", err)
	}
}

func (c *Comm) Write(b []byte) (n int, err error) {
	return c.writeWithLimit(b, maxReadMessageSize)
}

func (c *Comm) writeWithLimit(b []byte, maxMessageSize int) (n int, err error) {
	if err = validateMessageSize(len(b), maxMessageSize); err != nil {
		return 0, err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var header [8]byte
	copy(header[:4], MAGIC_BYTES)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(b)))
	buffers := net.Buffers{header[:], b}
	written, err := buffers.WriteTo(c.connection)
	if err != nil {
		err = fmt.Errorf("connection write failed: %w", err)
		return
	}
	n = int(written)
	if written != int64(len(header)+len(b)) {
		err = fmt.Errorf("wanted to write %d but wrote %d", len(header)+len(b), written)
		return
	}
	return
}

func validateMessageSize(size, maxMessageSize int) error {
	if size > maxMessageSize {
		return fmt.Errorf("%w: %d > %d", ErrMessageTooLarge, size, maxMessageSize)
	}
	return nil
}

func (c *Comm) Read() (buf []byte, numBytes int, bs []byte, err error) {
	return c.readWithDeadlineInto(nil, time.Now().Add(3*time.Hour), maxReadMessageSize)
}

func (c *Comm) readWithDeadline(readDeadline time.Time) (buf []byte, numBytes int, bs []byte, err error) {
	return c.readWithDeadlineInto(nil, readDeadline, maxReadMessageSize)
}

func (c *Comm) readWithDeadlineInto(dst []byte, readDeadline time.Time, maxMessageSize int) (buf []byte, numBytes int, bs []byte, err error) {
	if maxMessageSize < 0 || uint64(maxMessageSize) > uint64(^uint32(0)) {
		return nil, 0, nil, fmt.Errorf("invalid maximum message size %d", maxMessageSize)
	}
	// Clear stale connection deadlines before applying the deadline for this
	// read. The previous ordering cleared the read deadline immediately after
	// setting it, leaving header reads unbounded.
	if err = c.connection.SetDeadline(time.Time{}); err != nil {
		err = fmt.Errorf("failed to clear deadline: %w", err)
		return
	}
	if err = c.connection.SetReadDeadline(readDeadline); err != nil {
		err = fmt.Errorf("error setting read deadline: %w", err)
		return
	}

	var header [8]byte
	_, err = io.ReadFull(c.connection, header[:])
	if err != nil {
		log.Debugf("initial read error: %v", err)
		return
	}
	if !bytes.Equal(header[:4], MAGIC_BYTES) {
		err = fmt.Errorf("initial bytes are not magic: %x", header[:4])
		return
	}

	numBytesUint32 := binary.LittleEndian.Uint32(header[4:])
	if numBytesUint32 > uint32(maxMessageSize) {
		err = fmt.Errorf("%w: %d > %d", ErrMessageTooLarge, numBytesUint32, maxMessageSize)
		log.Debug(err.Error())
		return
	}
	numBytes = int(numBytesUint32)

	// Shorten the reading deadline in case getting weird data, while still
	// allowing large resume-control messages to cross slower relays.
	bodyReadDeadline := time.Now().Add(messageBodyReadTimeout)
	if !readDeadline.IsZero() && readDeadline.Before(bodyReadDeadline) {
		bodyReadDeadline = readDeadline
	}
	if err = c.connection.SetReadDeadline(bodyReadDeadline); err != nil {
		err = fmt.Errorf("error setting message body read deadline: %w", err)
		return
	}
	if cap(dst) < numBytes {
		buf = make([]byte, numBytes)
	} else {
		buf = dst[:numBytes]
	}
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

// ReceiveInto receives a framed message, reusing dst when it has enough
// capacity. Callers must finish using the returned bytes before the next call.
func (c *Comm) ReceiveInto(dst []byte) (b []byte, err error) {
	b, _, _, err = c.readWithDeadlineInto(dst, time.Now().Add(3*time.Hour), maxReadMessageSize)
	return
}

// ReceiveWithDeadline receives one framed message without allowing any part
// of the read to extend beyond the supplied absolute deadline.
func (c *Comm) ReceiveWithDeadline(deadline time.Time) (b []byte, err error) {
	b, _, _, err = c.readWithDeadline(deadline)
	return
}

// ReceiveWithDeadlineLimit receives one framed message without allowing any
// part of the read to extend beyond deadline or allocating more than maxSize
// bytes for the message body.
func (c *Comm) ReceiveWithDeadlineLimit(deadline time.Time, maxSize int) (b []byte, err error) {
	b, _, _, err = c.readWithDeadlineInto(nil, deadline, maxSize)
	return
}
