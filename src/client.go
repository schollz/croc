package croc

import (
	"errors"
	"net/url"
	"os"
	"os/signal"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/pake"
)

func (c *Croc) client(role int, codePhrase string) (err error) {
	defer log.Flush()

	// initialize the channel data for this client
	c.cs.Lock()

	c.cs.channel.codePhrase = codePhrase
	if len(codePhrase) > 0 {
		if len(codePhrase) < 4 {
			err = errors.New("code phrase must be more than 4 characters")
			return
		}
		c.cs.channel.Channel = codePhrase[:3]
		c.cs.channel.passPhrase = codePhrase[3:]
	} else {
		// TODO
		// generate code phrase
		c.cs.channel.Channel = "chou"
		c.cs.channel.passPhrase = codePhrase[3:]
	}
	channel := c.cs.channel.Channel
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
			log.Debugf("recv: %s", cd)
			err = c.processState(ws, cd)
			if err != nil {
				log.Warn(err)
				return
			}
		}
	}()

	// initialize by joining as corresponding role
	// TODO:
	// allowing suggesting a channel
	p := channelData{
		Open:    true,
		Role:    role,
		Channel: channel,
	}
	log.Debugf("sending opening payload: %+v", p)
	err = ws.WriteJSON(p)
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
			errWrite := ws.WriteJSON(channelData{
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

func (c *Croc) processState(ws *websocket.Conn, cd channelData) (err error) {
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
		c.cs.channel.Channel = cd.Channel
		c.cs.channel.Role = cd.Role
		c.cs.channel.Curve = cd.Curve
		c.cs.channel.Pake, err = pake.Init([]byte(c.cs.channel.passPhrase), cd.Role, getCurve(cd.Curve))
		c.cs.channel.Update = true
		log.Debugf("updating channel")
		errWrite := ws.WriteJSON(c.cs.channel)
		if errWrite != nil {
			log.Error(errWrite)
		}
		c.cs.channel.Update = false
		log.Debugf("initialized client state")
		return
	}
	// copy over the rest of the state
	if cd.TransferReady {
		c.cs.channel.TransferReady = true
	}
	c.cs.channel.Ports = cd.Ports
	if cd.Pake != nil && cd.Pake.Role != c.cs.channel.Role {
		log.Debugf("updating pake from %d", cd.Pake.Role)
		if c.cs.channel.Pake.HkA == nil {
			err = c.cs.channel.Pake.Update(cd.Pake.Bytes())
			if err != nil {
				log.Error(err)
				log.Debug("sending close signal")
				c.cs.channel.Close = true
				c.cs.channel.Error = err.Error()
				ws.WriteJSON(c.cs.channel)
				return
			}
			c.cs.channel.Update = true
			log.Debugf("updating channel")
			errWrite := ws.WriteJSON(c.cs.channel)
			if errWrite != nil {
				log.Error(errWrite)
			}
			c.cs.channel.Update = false
		}
	}

	// TODO:
	// process the client state
	log.Debugf("processing client state: %+v", c.cs.channel.String2())
	if c.cs.channel.Role == 0 {
		// processing for sender
	} else if c.cs.channel.Role == 1 {
		// processing for recipient
	}
	return
}
