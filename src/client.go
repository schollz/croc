package croc

import (
	"errors"
	"net/url"
	"os"
	"os/signal"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
)

func (c *Croc) client(role int) (err error) {
	defer log.Flush()

	// initialize the channel data for this client
	c.cs.Lock()
	c.cs.channel = newChannelData("")
	c.cs.Unlock()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// connect to the websocket
	// TODO:
	// use predefined host and HTTPS, if exists
	u := url.URL{Scheme: "ws", Host: "localhost:8003", Path: "/"}
	log.Debugf("connecting to %s", u.String())
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Error("dial:", err)
		return
	}
	defer ws.Close()

	// read in the messages and process them
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var cd channelData
			err := ws.ReadJSON(&cd)
			if err != nil {
				log.Debugf("sender read error:", err)
				return
			}
			log.Debugf("recv: %+v", cd)
			err = c.processState(cd)
			if err != nil {
				log.Warn(err)
				return
			}
		}
	}()

	// initialize by joining as corresponding role
	// TODO:
	// allowing suggesting a channel
	err = ws.WriteJSON(payload{
		Open: true,
		Role: role,
	})
	if err != nil {
		log.Errorf("problem opening: %s", err.Error())
		return
	}

	for {
		select {
		case <-done:
			return
		case <-interrupt:
			// send Close signal to relay on interrupt
			log.Debugf("interrupt")
			c.cs.Lock()
			channel := c.cs.channel.Channel
			uuid := c.cs.channel.UUID
			c.cs.Unlock()
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			log.Debug("sending close signal")
			errWrite := ws.WriteJSON(payload{
				Channel: channel,
				UUID:    uuid,
				Close:   true,
			})
			if errWrite != nil {
				log.Debugf("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
	return
}

func (c *Croc) processState(cd channelData) (err error) {
	c.cs.Lock()
	defer c.cs.Unlock()

	// first check if there is relay reported error
	if cd.Error != "" {
		err = errors.New(cd.Error)
		return
	}
	// TODO:
	// check if the state is not aligned (i.e. have h(k) but no hh(k))
	// throw error if not aligned so it can exit

	// first update the channel data
	// initialize if has UUID
	if cd.UUID != "" {
		c.cs.channel.UUID = cd.UUID
		c.cs.channel.Ports = cd.Ports
		c.cs.channel.Channel = cd.Channel
		c.cs.channel.Role = cd.Role
		c.cs.channel.Ports = cd.Ports
		log.Debugf("initialized client state")
	}
	// copy over the rest of the state
	if cd.TransferReady {
		c.cs.channel.TransferReady = true
	}
	for key := range cd.State {
		c.cs.channel.State[key] = cd.State[key]
	}

	// TODO:
	// process the client state
	log.Debugf("processing client state: %+v", c.cs.channel)
	return
}
