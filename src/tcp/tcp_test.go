package tcp

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func BenchmarkConnection(b *testing.B) {
	log.SetLevel("trace")
	go Run("debug", "127.0.0.1", "8283", "pass123", "8284")
	time.Sleep(100 * time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _, _, _ := ConnectToTCPServer("127.0.0.1:8283", "pass123", fmt.Sprintf("testroom%d", i), 1*time.Minute)
		if c != nil {
			c.Close()
		}
	}
}

func TestTCP(t *testing.T) {
	log.SetLevel("error")
	timeToRoomDeletion := 100 * time.Millisecond
	go RunWithOptionsAsync("127.0.0.1", "8381", "pass123",
		WithBanner("8382"),
		WithLogLevel("debug"),
		WithRoomTTL(timeToRoomDeletion))

	time.Sleep(timeToRoomDeletion)
	err := PingServer("127.0.0.1:8381")
	assert.Nil(t, err)
	err = PingServer("127.0.0.1:8333")
	assert.NotNil(t, err)

	time.Sleep(timeToRoomDeletion)
	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Minute)
	assert.Equal(t, banner, "8382")
	assert.Nil(t, err)
	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.Nil(t, err)
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.NotNil(t, err)
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Nanosecond)
	assert.NotNil(t, err)

	// try sending data
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	var data []byte
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

	c1.Close()
	time.Sleep(300 * time.Millisecond)
}

func TestTCPctx(t *testing.T) {
	log.SetLevel("error")
	// Set short room TTL for testing cleanup
	timeToRoomDeletion := 100 * time.Millisecond

	// Create cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server with custom options
	go RunWithOptionsAsync("127.0.0.1", "8381", "pass123",
		WithBanner("8382"),
		WithLogLevel("debug"),
		WithRoomTTL(timeToRoomDeletion),
		WithCtx(ctx),
	)

	time.Sleep(timeToRoomDeletion)

	// Test ping to running server
	err := PingServer("127.0.0.1:8381")
	assert.Nil(t, err)

	// Test ping to non-existent server
	err = PingServer("127.0.0.1:8333")
	assert.NotNil(t, err)

	time.Sleep(timeToRoomDeletion)

	// Connect first client to room
	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Minute)
	assert.Equal(t, banner, "8382")
	assert.Nil(t, err)

	// Connect second client to same room
	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.Nil(t, err)

	// Third client should fail - room is full
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.NotNil(t, err)

	// Connection with very short timeout should fail
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Nanosecond)
	assert.NotNil(t, err)

	// Test data exchange between clients
	// Send from c1 to c2
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	var data []byte
	for {
		data, err = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue // Skip heartbeat
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c2"), data)

	// Send from c2 to c1
	assert.Nil(t, c2.Send([]byte("hello, c1")))
	for {
		data, err = c1.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue // Skip heartbeat
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c1"), data)

	// Close server
	cancel()

	// Test ping to non-existent server
	err = PingServer("127.0.0.1:8331")
	assert.NotNil(t, err)

	time.Sleep(300 * time.Millisecond)
}

func TestWrongPassword(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8385", "pass123", "8386")
	time.Sleep(100 * time.Millisecond)

	// Attempt to connect with wrong password
	_, _, _, err := ConnectToTCPServer("127.0.0.1:8385", "wrongpass", "testRoom")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "bad password")
}

func TestRoomIsolation(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8387", "pass123", "8388")
	time.Sleep(100 * time.Millisecond)

	// Room 1
	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room1")
	c2, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room1")

	// Room 2
	c3, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room2")
	c4, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room2")

	// Send data in different rooms
	c1.Send([]byte("to_room_1"))
	c3.Send([]byte("to_room_2"))

	// Verify reception
	var data []byte

	// c2 should receive message from room1
	for {
		data, _ = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Equal(t, []byte("to_room_1"), data)

	// c4 should receive message from room2
	for {
		data, _ = c4.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Equal(t, []byte("to_room_2"), data)

	c1.Close()
	c2.Close()
	c3.Close()
	c4.Close()
}

func TestRoomRecreationAfterTTL(t *testing.T) {
	log.SetLevel("error")
	shortTTL := 50 * time.Millisecond

	go RunWithOptionsAsync("127.0.0.1", "8389", "pass123",
		WithRoomTTL(shortTTL),
		WithLogLevel("error"))
	time.Sleep(100 * time.Millisecond)

	roomName := "testRoomRecreate"

	// 1. Create a room
	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8389", "pass123", roomName)
	assert.NotNil(t, c1)

	// 2. Close first client, room becomes empty
	c1.Close()

	// 3. Wait for room cleanup (TTL + buffer)
	time.Sleep(shortTTL + 50*time.Millisecond)

	// 4. Try to connect to the same room again.
	// If room wasn't deleted, we might get "room full" or weird behavior.
	// If deleted — connection should succeed as the first client.
	c3, _, _, err := ConnectToTCPServer("127.0.0.1:8389", "pass123", roomName)
	assert.Nil(t, err)
	assert.NotNil(t, c3)

	if c3 != nil {
		c3.Close()
	}
}

func TestLargeDataTransfer(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8391", "pass123", "8392")
	time.Sleep(100 * time.Millisecond)

	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8391", "pass123", "bigRoom")
	c2, _, _, _ := ConnectToTCPServer("127.0.0.1:8391", "pass123", "bigRoom")

	// Generate data larger than standard buffer (e.g., 1 MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err := c1.Send(largeData)
	assert.Nil(t, err)

	var received []byte
	// Receive data, as it might arrive in chunks (though chanFromConn buffers it)
	// In this case pipe passes full Read packets, but for safety let's verify tail
	for {
		data, err := c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		assert.Nil(t, err)
		received = data
		break
	}

	assert.True(t, bytes.Equal(largeData, received), "Large data mismatch")

	c1.Close()
	c2.Close()
}

func TestServerReleasesPort(t *testing.T) {
	log.SetLevel("trace")
	host := "127.0.0.1"
	port := "8394"

	// 1. Start and automatically stop first server using timeout
	// RunCtx blocks the execution, so we don't need 'go' or channels
	ctx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()

	err := RunCtx(ctx1, "trace", host, port, "pass123")
	assert.Nil(t, err, "First server should stop gracefully")

	// 2. Try to start second server on the same port immediately
	// If port is not released, this will fail with "address already in use"
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()

	err = RunCtx(ctx2, "trace", host, port, "pass123")
	assert.Nil(t, err, "Second server should start (port was released)")
}
