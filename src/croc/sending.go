package croc

import (
	"os"
	"os/signal"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/recipient"
	"github.com/schollz/croc/src/relay"
	"github.com/schollz/croc/src/sender"
)

// Send the file
func (c *Croc) Send(fname, codephrase string) (err error) {
	log.Debugf("sending %s", fname)
	return c.sendReceive(fname, codephrase, true)
}

// Receive the file
func (c *Croc) Receive(codephrase string) (err error) {
	return c.sendReceive("", codephrase, false)
}

func (c *Croc) sendReceive(fname, codephrase string, isSender bool) (err error) {
	defer log.Flush()

	// allow interrupts
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// connect to server
	log.Debugf("connecting to %s", c.WebsocketAddress)
	sock, _, err := websocket.DefaultDialer.Dial(c.WebsocketAddress+"/ws", nil)
	if err != nil {
		return
	}
	defer sock.Close()

	done := make(chan struct{})

	// tell the websockets we are connected
	err = sock.WriteMessage(websocket.BinaryMessage, []byte("connected"))
	if err != nil {
		return err
	}

	if isSender {
		go sender.Send(done, sock, fname, codephrase)
	} else {
		go recipient.Receive(done, sock, codephrase)
	}

	for {
		select {
		case <-done:
			return nil
		case <-interrupt:
			log.Debug("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := sock.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Debug("write close:", err)
				return nil
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return nil
		}
	}
}

// Relay will start a relay on the specified port
func (c *Croc) Relay() (err error) {
	return relay.Run(c.ServerPort)
}
