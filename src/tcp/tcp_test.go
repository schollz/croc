package tcp

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/schollz/croc/src/comm"
	"github.com/stretchr/testify/assert"
)

func TestTCP(t *testing.T) {
	go Run("debug", "8081")
	time.Sleep(100 * time.Millisecond)
	c1, err := ConnectToTCPServer("localhost:8081", "testRoom")
	assert.Nil(t, err)
	c2, err := ConnectToTCPServer("localhost:8081", "testRoom")
	assert.Nil(t, err)
	_, err = ConnectToTCPServer("localhost:8081", "testRoom")
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
	time.Sleep(200 * time.Millisecond)
	err = c2.Send([]byte("test"))
	assert.Nil(t, err)
	_, err = c2.Receive()
	assert.NotNil(t, err)
}

func ConnectToTCPServer(address, room string) (c comm.Comm, err error) {
	c, err = comm.NewConnection("localhost:8081")
	if err != nil {
		return
	}
	data, err := c.Receive()
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte("ok")) {
		err = fmt.Errorf("got bad response: %s", data)
		return
	}
	err = c.Send([]byte(room))
	if err != nil {
		return
	}
	data, err = c.Receive()
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte("ok")) {
		err = fmt.Errorf("got bad response: %s", data)
		return
	}
	return
}
