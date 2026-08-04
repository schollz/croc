package comm

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"net"
	"testing"
	"time"

	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func TestComm(t *testing.T) {
	token := make([]byte, 3000)
	if _, err := rand.Read(token); err != nil {
		t.Error(err)
	}

	// Use dynamic port allocation to avoid conflicts
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	portStr := listener.Addr().String()
	listener.Close() // Close the listener so we can reopen it in the goroutine

	go func() {
		log.Debug("starting TCP server on " + portStr)
		server, err := net.Listen("tcp", portStr)
		if err != nil {
			log.Error(err)
			return
		}
		defer func() {
			if err := server.Close(); err != nil {
				log.Error(err)
			}
		}()
		// spawn a new goroutine whenever a client connects
		for {
			connection, err := server.Accept()
			if err != nil {
				log.Error(err)
			}
			log.Debugf("client %s connected", connection.RemoteAddr().String())
			go func(_ int, connection net.Conn) {
				c := New(connection)
				err = c.Send([]byte("hello, world"))
				assert.Nil(t, err)
				data, err := c.Receive()
				assert.Nil(t, err)
				assert.Equal(t, []byte("hello, computer"), data)
				data, err = c.Receive()
				assert.Nil(t, err)
				assert.Equal(t, []byte{'\x00'}, data)
				data, err = c.Receive()
				assert.Nil(t, err)
				assert.Equal(t, token, data)
			}(port, connection)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	a, err := NewConnection(portStr, 10*time.Minute)
	assert.Nil(t, err)
	data, err := a.Receive()
	assert.Equal(t, []byte("hello, world"), data)
	assert.Nil(t, err)
	assert.Nil(t, a.Send([]byte("hello, computer")))
	assert.Nil(t, a.Send([]byte{'\x00'}))

	assert.Nil(t, a.Send(token))
	_ = a.Connection()
	a.Close()
	assert.NotNil(t, a.Send(token))
	_, err = a.Write(token)
	assert.NotNil(t, err)
}

func TestReceiveRejectsOversizedMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := New(clientConn)

	writeErr := make(chan error, 1)
	go func() {
		header := new(bytes.Buffer)
		header.Write(MAGIC_BYTES)
		if err := binary.Write(header, binary.LittleEndian, uint32(maxReadMessageSize+1)); err != nil {
			writeErr <- err
			return
		}
		_, err := serverConn.Write(header.Bytes())
		writeErr <- err
	}()

	_, err := c.Receive()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "message too large")
	assert.Nil(t, <-writeErr)
}

func TestReceiveAllowsLargeMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := New(clientConn)
	payload := bytes.Repeat([]byte("a"), 2*1024*1024)

	writeErr := make(chan error, 1)
	go func() {
		_, err := New(serverConn).Write(payload)
		writeErr <- err
	}()

	data, err := c.Receive()
	assert.Nil(t, err)
	assert.Equal(t, payload, data)
	assert.Nil(t, <-writeErr)
	assert.GreaterOrEqual(t, messageBodyReadTimeout, time.Minute)
}

func TestReceiveWithDeadlineTimesOutWhileReadingHeader(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := New(clientConn)
	_, err := c.ReceiveWithDeadline(time.Now().Add(50 * time.Millisecond))
	assertTimeoutError(t, err)
}

func TestReceiveWithDeadlineClampsMessageBodyRead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := New(clientConn)
	writeErr := make(chan error, 1)
	go func() {
		header := new(bytes.Buffer)
		header.Write(MAGIC_BYTES)
		if err := binary.Write(header, binary.LittleEndian, uint32(4)); err != nil {
			writeErr <- err
			return
		}
		header.WriteByte('a')
		_, err := serverConn.Write(header.Bytes())
		writeErr <- err
	}()

	_, err := c.ReceiveWithDeadline(time.Now().Add(50 * time.Millisecond))
	assertTimeoutError(t, err)
	assert.Nil(t, <-writeErr)
}

func TestReceiveWithDeadlineRemainsAbsoluteAcrossMessages(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := New(clientConn)
	deadline := time.Now().Add(250 * time.Millisecond)
	writeErr := make(chan error, 1)
	go func() {
		_, err := New(serverConn).Write([]byte("first"))
		writeErr <- err
	}()

	data, err := c.ReceiveWithDeadline(deadline)
	assert.NoError(t, err)
	assert.Equal(t, []byte("first"), data)
	assert.NoError(t, <-writeErr)

	time.Sleep(150 * time.Millisecond)
	started := time.Now()
	_, err = c.ReceiveWithDeadline(deadline)
	assertTimeoutError(t, err)
	assert.Less(t, time.Since(started), 175*time.Millisecond)
}

func assertTimeoutError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	netErr, ok := err.(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("expected a network timeout, got %T: %v", err, err)
	}
}
