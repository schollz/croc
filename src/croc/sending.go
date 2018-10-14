package croc

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/relay"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/utils"
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
			errChan <- c.sendReceive(c.Address, c.AddressWebsocketPort, c.AddressTCPPorts, fname, codephrase, true, false)
		}()
	} else {
		waitingFor = 1
	}

	// use local relay
	if !c.NoLocal {
		go func() {
			// start own relay and connect to it
			go relay.Run(c.RelayWebsocketPort, c.RelayTCPPorts)
			time.Sleep(250 * time.Millisecond) // race condition here, but this should work most of the time :(

			// broadcast for peer discovery
			go func() {
				log.Debug("starting local discovery...")
				discovered, err := peerdiscovery.Discover(peerdiscovery.Settings{
					Limit:     1,
					TimeLimit: 600 * time.Second,
					Delay:     50 * time.Millisecond,
					Payload:   []byte(c.RelayWebsocketPort + "- " + strings.Join(c.RelayTCPPorts, ",")),
				})
				log.Debug(discovered, err)
			}()

			// connect to own relay
			errChan <- c.sendReceive("localhost", c.RelayWebsocketPort, c.RelayTCPPorts, fname, codephrase, true, true)
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
		log.Debug("trying discovering")
		// try to discovery codephrase and server through peer network
		discovered, errDiscover := peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:            1,
			TimeLimit:        300 * time.Millisecond,
			Delay:            50 * time.Millisecond,
			Payload:          []byte("checking"),
			AllowSelf:        true,
			DisableBroadcast: true,
		})
		log.Debug("finished")
		log.Debug(discovered)
		if errDiscover != nil {
			log.Debug(errDiscover)
		}
		if len(discovered) > 0 {
			if discovered[0].Address == utils.GetLocalIP() {
				discovered[0].Address = "localhost"
			}
			log.Debugf("discovered %s:%s", discovered[0].Address, discovered[0].Payload)
			// see if we can actually connect to it
			timeout := time.Duration(200 * time.Millisecond)
			client := http.Client{
				Timeout: timeout,
			}
			ports := strings.Split(string(discovered[0].Payload), "-")
			if len(ports) != 2 {
				return errors.New("bad payload")
			}
			resp, err := client.Get(fmt.Sprintf("http://%s:%s/", discovered[0].Address, ports[0]))
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					// we connected, so use this
					return c.sendReceive(discovered[0].Address, strings.TrimSpace(ports[0]), strings.Split(strings.TrimSpace(ports[1]), ","), "", codephrase, false, true)
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
		return c.sendReceive(c.Address, c.AddressWebsocketPort, c.AddressTCPPorts, "", codephrase, false, false)
	}

	return errors.New("must use local or public relay")
}

func (c *Croc) sendReceive(address, websocketPort string, tcpPorts []string, fname string, codephrase string, isSender bool, isLocal bool) (err error) {
	defer log.Flush()
	if len(codephrase) < 4 {
		return fmt.Errorf("codephrase is too short")
	}

	// allow interrupts from Ctl+C
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	done := make(chan struct{})
	// connect to server
	websocketAddress := ""
	if len(websocketPort) > 0 {
		websocketAddress = fmt.Sprintf("ws://%s:%s/ws?room=%s", address, websocketPort, codephrase[:3])
	} else {
		websocketAddress = fmt.Sprintf("ws://%s/ws?room=%s", address, codephrase[:3])
	}
	log.Debugf("connecting to %s", websocketAddress)
	sock, _, err := websocket.DefaultDialer.Dial(websocketAddress, nil)
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
		go c.startSender(c.ForceSend, address, tcpPorts, isLocal, done, sock, fname, codephrase, c.UseCompression, c.UseEncryption)
	} else {
		go c.startRecipient(c.ForceSend, address, tcpPorts, isLocal, done, sock, codephrase, c.NoRecipientPrompt, c.Stdout)
	}

	for {
		select {
		case <-done:
			return nil
		case <-interrupt:
			if !c.Debug {
				SetDebugLevel("critical")
			}
			log.Debug("interrupt")
			err = sock.WriteMessage(websocket.TextMessage, []byte("interrupt"))
			if err != nil {
				return err
			}
			time.Sleep(50 * time.Millisecond)

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := sock.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Debug("write close:", err)
				return nil
			}
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
			return nil
		}
	}
}

// Relay will start a relay on the specified port
func (c *Croc) Relay() (err error) {
	return relay.Run(c.RelayWebsocketPort, c.RelayTCPPorts)
}
