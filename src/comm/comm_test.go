package comm

import (
	"crypto/rand"
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
