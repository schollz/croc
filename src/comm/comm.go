package comm

import (
	"net"
	"strings"
	"time"

	"github.com/schollz/croc/src/models"
)

// Comm is some basic TCP communication
type Comm struct {
	connection net.Conn
}

// New returns a new comm
func New(c net.Conn) Comm {
	return Comm{c}
}

// Connection returns the net.Conn connection
func (c Comm) Connection() net.Conn {
	return c.connection
}

func (c Comm) Write(b []byte) (int, error) {
	return c.connection.Write(b)
}

func (c Comm) Read() (buf []byte, err error) {
	buf = make([]byte, models.WEBSOCKET_BUFFER_SIZE)
	n, err := c.connection.Read(buf)
	buf = buf[:n]
	return
}

// Send a message
func (c Comm) Send(message string) (err error) {
	message = fillString(message, models.TCP_BUFFER_SIZE)
	_, err = c.connection.Write([]byte(message))
	return
}

// Receive a message
func (c Comm) Receive() (s string, err error) {
	messageByte := make([]byte, models.TCP_BUFFER_SIZE)
	err = c.connection.SetReadDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	err = c.connection.SetDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	err = c.connection.SetWriteDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	_, err = c.connection.Read(messageByte)
	if err != nil {
		return
	}
	s = strings.TrimRight(string(messageByte), ":")
	return
}

func fillString(returnString string, toLength int) string {
	for {
		lengthString := len(returnString)
		if lengthString < toLength {
			returnString = returnString + ":"
			continue
		}
		break
	}
	return returnString
}
