package tcp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/comm"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func BenchmarkConnection(b *testing.B) {
	log.SetLevel("trace")
	go Run("debug", "127.0.0.1", "8283", "pass123", "8284")
	time.Sleep(100 * time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _, _, _ := ConnectToTCPServer("127.0.0.1:8283", "pass123", fmt.Sprintf("testroom%d", i), true, true, 1, 1*time.Minute)
		c.Close()
	}
}

func TestTCPServerPing(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)
	err := PingServer("127.0.0.1:8381")
	assert.Nil(t, err)
	err = PingServer("127.0.0.1:8333")
	assert.NotNil(t, err)
}

// This is helper function to test that a mocks a transfer
// between two clients connected to the server,
// and checks that the data is transferred correctly
func mockTransfer(c1, c2 *comm.Comm, t *testing.T) {
	// try sending data to check the pipe is working properly
	var data []byte
	var err error
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	for {
		data, err = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c2"), data)

	assert.Nil(t, c2.Send([]byte("hello, c1")))
	for {
		data, err = c1.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c1"), data)
}

// Test that a successful transfer can be made
func TestTCPServerSingleConnectionTransfer(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 1, 1*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c1)
	assert.Equal(t, banner, "8382")

	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1)
	assert.Nil(t, err)
	assert.NotNil(t, c2)

	mockTransfer(c1, c2, t)

	c1.Close()
	c2.Close()
	time.Sleep(300 * time.Millisecond)
}

// Test that a receiver can connect before a sender
func TestTCPRecieverFirst(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	receiver, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 1*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, receiver)
	assert.Equal(t, banner, "8382")

	sender, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 1, 1*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, sender)

	mockTransfer(receiver, sender, t)

	receiver.Close()
	sender.Close()
	time.Sleep(300 * time.Millisecond)
}

// Test that a third client cannot connect
// to a room that already has two clients
// connected to it with maxTransfers=1
func TestTCPSingleConnectionOnly2Clients(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c1)
	assert.Equal(t, banner, "8382")

	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c2)
	closeChan := make(chan int)

	// we need to run this transfer in a goroutine because
	// otherwise connections will be idle and the server will
	// close them when we try to connect a third client
	go func() {
		for {
			select {
			case <-closeChan:
				fmt.Println("Closing go routine")
				return
			default:
				mockTransfer(c1, c2, t)
			}
		}
	}()

	c3, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 5*time.Minute)
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "room is full"))
	assert.Nil(t, c3)
	closeChan <- 1
}

// Test that the server can handle multiple
// successive transfers from the same sender
func TestTCPMultipleConnectionTransfer(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 2, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c1)
	assert.Equal(t, banner, "8382")

	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c2)

	mockTransfer(c1, c2, t)
	c2.Close()
	// tell c1 to close pipe listener
	c1.Send([]byte("finished"))
	time.Sleep(100 * time.Millisecond)

	c3, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 5*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c3)

	mockTransfer(c1, c3, t)
}

// Test that for a room with maxTransfers>=2,
// the receivers are queued if there is a transfer
// in progress already, and the receiver is allowed
// to connect when the transfer is finished
func TestTCPMultipleConnectionWaitingRoom(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 2, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c1)
	assert.Equal(t, banner, "8382")

	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c2)

	// we need to run this transfer in a goroutine because
	// otherwise connections will be idle and the server will
	// close them when we try to connect a third client
	go func() {
		counter := 1
		time.Sleep(100 * time.Millisecond)
		for {
			mockTransfer(c1, c2, t)
			if counter == 5 {
				c2.Close()
				// tell c1 to close pipe listener
				c1.Send([]byte("finished"))
				break
			}
			counter++
		}
	}()

	c3, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 5*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c3)

	mockTransfer(c1, c3, t)
}

// Test that for a room with maxTransfers>=2,
// if there are receivers queued they will get a
// nottification that the room is no longer available
// when the sender the maxTransfers limit is reached
func TestTCPMultipleConnectionWaitingRoomCloses(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8381", "pass123", "8382")
	time.Sleep(100 * time.Millisecond)

	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", true, true, 2, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c1)
	assert.Equal(t, banner, "8382")

	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c2)

	// one transfer
	mockTransfer(c1, c2, t)
	c2.Close()
	// tell c1 to close pipe listener
	c1.Send([]byte("finished"))

	c2, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 10*time.Minute)
	assert.Nil(t, err)
	assert.NotNil(t, c2)

	go func() {
		c3, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", false, true, 1, 5*time.Minute)
		assert.NotNil(t, err)
		assert.True(t, strings.Contains(err.Error(), "sender is gone"))
		assert.Nil(t, c3)
	}()

	time.Sleep(100 * time.Millisecond)
	c2.Close()
	// tell c1 to close pipe listener
	c1.Send([]byte("finished"))
}
