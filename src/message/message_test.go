package message

import (
	"crypto/rand"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/comm"
	"github.com/schollz/croc/v9/src/crypt"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func TestMessage(t *testing.T) {
	log.SetLevel("debug")
	m := Message{Type: "message", Message: "hello, world"}
	e, salt, err := crypt.New([]byte("pass"), nil)
	assert.Nil(t, err)
	fmt.Println(salt)
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
	m := Message{Type: "message", Message: "hello, world"}
	b, err := Encode(nil, m)
	assert.Nil(t, err)
	fmt.Printf("%x\n", b)

	m2, err := Decode(nil, b)
	assert.Nil(t, err)
	assert.Equal(t, m, m2)
	assert.Equal(t, `{"t":"message","m":"hello, world"}`, m.String())
}

func TestSend(t *testing.T) {
	token := make([]byte, 40000000)
	rand.Read(token)

	port := "8801"
	go func() {
		log.Debugf("starting TCP server on " + port)
		server, err := net.Listen("tcp", "0.0.0.0:"+port)
		if err != nil {
			log.Error(err)
		}
		defer server.Close()
		// spawn a new goroutine whenever a client connects
		for {
			connection, err := server.Accept()
			if err != nil {
				log.Error(err)
			}
			log.Debugf("client %s connected", connection.RemoteAddr().String())
			go func(port string, connection net.Conn) {
				c := comm.New(connection)
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
	a, err := comm.NewConnection("localhost:"+port, 10*time.Minute)
	assert.Nil(t, err)
	m := Message{Type: "message", Message: "hello, world"}
	e, salt, err := crypt.New([]byte("pass"), nil)
	log.Debug(salt)
	assert.Nil(t, err)

	assert.Nil(t, Send(a, e, m))
}
