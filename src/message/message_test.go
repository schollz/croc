package message

import (
	"fmt"
	"net"
	"testing"

	"github.com/schollz/croc/v10/src/comm"
	"github.com/schollz/croc/v10/src/crypt"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

var TypeMessage Type = "message"

func TestMessage(t *testing.T) {
	log.SetLevel("debug")
	m := Message{Type: TypeMessage, Message: "hello, world"}
	e, salt, err := crypt.New([]byte("pass"), nil)
	assert.Nil(t, err)
	fmt.Println(string(salt))
	b, err := Encode(e, m)
	assert.Nil(t, err)
	fmt.Printf("%x\n", b)

	m2, err := Decode(e, b)
	assert.Nil(t, err)
	assert.Equal(t, m, m2)
	assert.Equal(t, `{"t":"message","m":"hello, world"}`, m.String())
	_, err = Decode([]byte("not pass"), b)
	assert.NotNil(t, err)
	_, err = Encode([]byte("0"), m)
	assert.NotNil(t, err)
}

func TestMessageNoPass(t *testing.T) {
	log.SetLevel("debug")
	m := Message{Type: TypeMessage, Message: "hello, world"}
	b, err := Encode(nil, m)
	assert.Nil(t, err)
	fmt.Printf("%x\n", b)

	m2, err := Decode(nil, b)
	assert.Nil(t, err)
	assert.Equal(t, m, m2)
	assert.Equal(t, `{"t":"message","m":"hello, world"}`, m.String())
}

func TestSend(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := comm.New(clientConn)
	server := comm.New(serverConn)
	want := Message{Type: TypeMessage, Message: "hello, world"}
	e, salt, err := crypt.New([]byte("pass"), nil)
	log.Debug(salt)
	if err != nil {
		t.Fatalf("create encryption key: %v", err)
	}

	type receiveResult struct {
		message Message
		err     error
	}
	resultCh := make(chan receiveResult, 1)
	go func() {
		data, err := server.Receive()
		if err != nil {
			resultCh <- receiveResult{err: err}
			return
		}
		got, err := Decode(e, data)
		resultCh <- receiveResult{message: got, err: err}
	}()

	if err := Send(client, e, want); err != nil {
		t.Fatalf("send message: %v", err)
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatalf("receive message: %v", result.err)
	}
	assert.Equal(t, want, result.message)
}
