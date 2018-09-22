package croc

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/recipient"
	"github.com/schollz/croc/src/relay"
	"github.com/schollz/croc/src/sender"
	"github.com/schollz/peerdiscovery"
)

// Send the file
func (c *Croc) Send(fname, codephrase string) (err error) {
	log.Debugf("sending %s", fname)
	errChan := make(chan error)

	// normally attempt two connections
	waitingFor := 2

	// use public relay
	if !c.LocalOnly {
		go func() {
			// atttempt to connect to public relay
			errChan <- c.sendReceive(c.WebsocketAddress, fname, codephrase, true)
		}()
	} else {
		waitingFor = 1
	}

	// use local relay
	if !c.NoLocal {
		go func() {
			// start own relay and connect to it
			go relay.Run(c.ServerPort)
			time.Sleep(250 * time.Millisecond) // race condition here, but this should work most of the time :(

			// broadcast for peer discovery
			go func() {
				log.Debug("listening for local croc relay...")
				go peerdiscovery.Discover(peerdiscovery.Settings{
					Limit:     1,
					TimeLimit: 600 * time.Second,
					Delay:     50 * time.Millisecond,
					Payload:   []byte(c.ServerPort),
				})
			}()

			// connect to own relay
			errChan <- c.sendReceive("ws://localhost:"+c.ServerPort, fname, codephrase, true)
		}()
	} else {
		waitingFor = 1
	}

	err = <-errChan
	if err == nil || waitingFor == 1 {
		log.Debug("returning")
		return
	}
	log.Debug(err)
	return <-errChan
}

// Receive the file
func (c *Croc) Receive(codephrase string) (err error) {
	log.Debug("receiving")

	// use local relay first
	if !c.NoLocal {
		// try to discovery codephrase and server through peer network
		discovered, errDiscover := peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     1,
			TimeLimit: 300 * time.Millisecond,
			Delay:     50 * time.Millisecond,
			Payload:   []byte("checking"),
		})
		if errDiscover != nil {
			log.Debug(errDiscover)
		}
		if len(discovered) > 0 {
			log.Debugf("discovered %s:%s", discovered[0].Address, discovered[0].Payload)
			// see if we can actually connect to it
			timeout := time.Duration(200 * time.Millisecond)
			client := http.Client{
				Timeout: timeout,
			}
			resp, err := client.Get(fmt.Sprintf("http://%s:%s/", discovered[0].Address, discovered[0].Payload))
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					// we connected, so use this
					return c.sendReceive(fmt.Sprintf("ws://%s:%s", discovered[0].Address, discovered[0].Payload), "", codephrase, false)
				}
			} else {
				log.Debugf("could not connect: %s", err.Error())
			}
		} else {
			log.Debug("discovered no peers")
		}
	}

	// use public relay
	if !c.LocalOnly {
		log.Debug("using public relay")
		return c.sendReceive(c.WebsocketAddress, "", codephrase, false)
	}

	return errors.New("must use local or public relay")
}

func (c *Croc) sendReceive(websocketAddress, fname, codephrase string, isSender bool) (err error) {
	defer log.Flush()
	if len(codephrase) < 4 {
		return fmt.Errorf("codephrase is too short")
	}

	// allow interrupts
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	done := make(chan struct{})
	// connect to server
	log.Debugf("connecting to %s", websocketAddress+"/ws?room="+codephrase[:3])
	sock, _, err := websocket.DefaultDialer.Dial(websocketAddress+"/ws?room="+codephrase[:3], nil)
	if err != nil {
		return
	}
	defer sock.Close()

	// tell the websockets we are connected
	err = sock.WriteMessage(websocket.BinaryMessage, []byte("connected"))
	if err != nil {
		return err
	}

	if isSender {
		// start peerdiscovery relay server
		go sender.Send(done, sock, fname, codephrase)
	} else {
		go recipient.Receive(done, sock, codephrase, c.NoRecipientPrompt, c.Stdout)
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
