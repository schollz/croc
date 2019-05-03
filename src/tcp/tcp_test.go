package tcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTCP(t *testing.T) {
	go Run("debug", "8081", "8082")
	time.Sleep(100 * time.Millisecond)
	c1, banner, _, err := ConnectToTCPServer("localhost:8081", "testRoom")
	assert.Equal(t, banner, "8082")
	assert.Nil(t, err)
	c2, _, _, err := ConnectToTCPServer("localhost:8081", "testRoom")
	assert.Nil(t, err)
	_, _, _, err = ConnectToTCPServer("localhost:8081", "testRoom")
	assert.NotNil(t, err)

	// try sending data
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	data, err := c2.Receive()
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c2"), data)

	assert.Nil(t, c2.Send([]byte("hello, c1")))
	data, err = c1.Receive()
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c1"), data)

	c1.Close()
	time.Sleep(300 * time.Millisecond)
}
