package tcp

import (
	"bytes"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/crypt"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

// func BenchmarkConnection(b *testing.B) {
// 	log.SetLevel("trace")
// 	go Run("debug", "8283", "pass123", "8284")
// 	time.Sleep(100 * time.Millisecond)
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		c, _, _, _ := ConnectToTCPServer("localhost:8283", "pass123", fmt.Sprintf("testroom%d", i), 1*time.Minute)
// 		c.Close()
// 	}
// }

func TestTCP(t *testing.T) {
	log.SetLevel("error")
	timeToRoomDeletion = 100 * time.Millisecond
	keyPublic, keyPrivate, err := crypt.NewAge()
	if err != nil {
		panic(err)
	}
	go Run("debug", "8281", "pass123", keyPublic, keyPrivate, "8282")
	time.Sleep(100 * time.Millisecond)
	err = PingServer("localhost:8281")
	assert.Nil(t, err)
	err = PingServer("localhost:8333")
	assert.NotNil(t, err)

	time.Sleep(100 * time.Millisecond)
	c1, banner, _, err := ConnectToTCPServer("localhost:8281", "pass123", keyPublic, "testRoom", 1*time.Minute)
	assert.Equal(t, banner, "8282")
	assert.Nil(t, err)
	c2, _, _, err := ConnectToTCPServer("localhost:8281", "pass123", keyPublic, "testRoom")
	assert.Nil(t, err)
	_, _, _, err = ConnectToTCPServer("localhost:8281", "pass123", keyPublic, "testRoom")
	assert.NotNil(t, err)
	_, _, _, err = ConnectToTCPServer("localhost:8281", "pass123", keyPublic, "testRoom", 1*time.Nanosecond)
	assert.NotNil(t, err)
	_, _, _, err = ConnectToTCPServer("localhost:8281", "pass123", keyPublic+"askldjfklsajdf", "testRoom2")
	assert.NotNil(t, err)
	_, _, _, err = ConnectToTCPServer("localhost:8281", "pass125", keyPublic, "testRoom3")
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
