package croc

import (
	"net/http"
	"time"

	log "github.com/cihub/seelog"
	"github.com/frankenbeanies/uuid4"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// startServer initiates the server which listens for websocket connections
func (c *Croc) startServer(tcpPorts []string, port string) (err error) {
	// start cleanup on dangling channels
	go c.channelCleanup()

	var upgrader = websocket.Upgrader{} // use default options
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// incoming websocket request
		ws, err := upgrader.Upgrade(w, r, nil)
		log.Debugf("connecting remote addr: %s", ws.RemoteAddr().String())
		if err != nil {
			log.Error("upgrade:", err)
			return
		}
		defer ws.Close()

		var channel string
		for {
			log.Debug("waiting for next message")
			var p payload
			err := ws.ReadJSON(&p)
			if err != nil {
				if _, ok := err.(*websocket.CloseError); ok {
					// on forced close, delete the channel
					log.Debug("closed channel")
					c.closeChannel(channel)
				} else {
					log.Debugf("read:", err)
				}
				break
			}
			channel, err = c.processPayload(ws, p)
			if err != nil {
				// if error, send the error back and then delete the channel
				log.Warn("problem processing payload %+v: %s", p, err.Error())
				ws.WriteJSON(channelData{Error: err.Error()})
				c.closeChannel(channel)
				return
			}
		}
	})
	log.Debugf("listening on port %s", c.ServerPort)
	err = http.ListenAndServe(":"+c.ServerPort, nil)
	return
}

func (c *Croc) updateChannel(p payload) (err error) {
	c.rs.Lock()
	defer c.rs.Unlock()

	// determine if channel is invalid
	if _, ok := c.rs.channel[p.Channel]; !ok {
		err = errors.Errorf("channel '%s' does not exist", p.Channel)
		return
	}

	// determine if UUID is invalid for channel
	if p.UUID != c.rs.channel[p.Channel].uuids[0] &&
		p.UUID != c.rs.channel[p.Channel].uuids[1] {
		err = errors.Errorf("uuid '%s' is invalid", p.UUID)
		return
	}

	// assign each key provided
	assignedKeys := []string{}
	for key := range p.State {
		// TODO:
		// add a check that the value of key is not enormous

		// add only if it is a valid key
		if _, ok := c.rs.channel[p.Channel].State[key]; ok {
			assignedKeys = append(assignedKeys, key)
			c.rs.channel[p.Channel].State[key] = p.State[key]
		}
	}

	log.Debugf("assigned %d keys: %v", len(assignedKeys), assignedKeys)
	return
}

func (c *Croc) joinChannel(ws *websocket.Conn, p payload) (channel string, err error) {
	log.Debugf("joining channel %s", ws.RemoteAddr().String())
	c.rs.Lock()
	defer c.rs.Unlock()

	// determine if sender or recipient
	if p.Role != 0 && p.Role != 1 {
		err = errors.Errorf("no such role of %d", p.Role)
		return
	}

	// determine channel
	if p.Channel == "" {
		// TODO:
		// find an empty channel
		p.Channel = "chou"
	}
	if _, ok := c.rs.channel[p.Channel]; ok {
		// channel is not empty
		if c.rs.channel[p.Channel].uuids[p.Role] != "" {
			err = errors.Errorf("channel '%s' already occupied by role %d", p.Channel, p.Role)
			return
		}
	}
	log.Debug("creating new channel")
	if _, ok := c.rs.channel[p.Channel]; !ok {
		c.rs.channel[p.Channel] = newChannelData(p.Channel)
	}
	channel = p.Channel

	// assign UUID for the role in the channel
	c.rs.channel[p.Channel].uuids[p.Role] = uuid4.New().String()
	log.Debugf("(%s) %s has joined as role %d", p.Channel, c.rs.channel[p.Channel].uuids[p.Role], p.Role)
	// send Channel+UUID back to the current person
	err = ws.WriteJSON(channelData{
		Channel: p.Channel,
		UUID:    c.rs.channel[p.Channel].uuids[p.Role],
		Role:    p.Role,
	})
	if err != nil {
		return
	}

	// if channel is not open, set initial parameters
	if !c.rs.channel[p.Channel].isopen {
		c.rs.channel[p.Channel].isopen = true
		c.rs.channel[p.Channel].Ports = c.TcpPorts
		c.rs.channel[p.Channel].startTime = time.Now()
		p.Curve, _ = getCurve(p.Curve)
		log.Debugf("(%s) using curve '%s'", p.Channel, p.Curve)
		c.rs.channel[p.Channel].State["curve"] = []byte(p.Curve)
	}
	c.rs.channel[p.Channel].websocketConn[p.Role] = ws

	log.Debugf("assigned role %d in channel '%s'", p.Role, p.Channel)
	return
}

// closeChannel will shut down current open websockets and delete the channel information
func (c *Croc) closeChannel(channel string) {
	c.rs.Lock()
	defer c.rs.Unlock()
	// check if channel exists
	if _, ok := c.rs.channel[channel]; !ok {
		return
	}
	// close open connections
	for _, wsConn := range c.rs.channel[channel].websocketConn {
		if wsConn != nil {
			wsConn.Close()
		}
	}
	// delete
	delete(c.rs.channel, channel)
}

func (c *Croc) processPayload(ws *websocket.Conn, p payload) (channel string, err error) {
	log.Debugf("processing payload from %s", ws.RemoteAddr().String())
	channel = p.Channel

	// if the request is to close, delete the channel
	if p.Close {
		log.Debugf("closing channel %s", p.Channel)
		c.closeChannel(p.Channel)
		return
	}

	// if request is to Open, try to open
	if p.Open {
		channel, err = c.joinChannel(ws, p)
		if err != nil {
			return
		}
	}

	// check if open, otherwise return error
	c.rs.Lock()
	if _, ok := c.rs.channel[channel]; ok {
		if !c.rs.channel[channel].isopen {
			err = errors.Errorf("channel %s is not open, need to open first", channel)
			c.rs.Unlock()
			return
		}
	}
	c.rs.Unlock()

	// if the request is to Update, then update the state
	if p.Update {
		// update
		err = c.updateChannel(p)
		if err != nil {
			return
		}
	}

	// TODO:
	// relay state logic here

	// send out the data to both sender + receiver each time
	c.rs.Lock()
	if _, ok := c.rs.channel[channel]; ok {
		for role, wsConn := range c.rs.channel[channel].websocketConn {
			if wsConn == nil {
				continue
			}
			log.Debugf("writing latest data %+v to %d", c.rs.channel[channel].String2(), role)
			err = wsConn.WriteJSON(c.rs.channel[channel])
			if err != nil {
				log.Debugf("problem writing to role %d: %s", role, err.Error())
			}
		}
	}
	c.rs.Unlock()
	return
}

func (c *Croc) channelCleanup() {
	maximumWait := 10 * time.Minute
	for {
		c.rs.Lock()
		keys := make([]string, len(c.rs.channel))
		i := 0
		for key := range c.rs.channel {
			keys[i] = key
			i++
		}
		channelsToDelete := []string{}
		for _, key := range keys {
			if time.Since(c.rs.channel[key].startTime) > maximumWait {
				channelsToDelete = append(channelsToDelete, key)
			}
		}
		c.rs.Unlock()

		for _, channel := range channelsToDelete {
			log.Debugf("channel %s has exceeded time, deleting", channel)
			c.closeChannel(channel)
		}
		time.Sleep(1 * time.Minute)
	}
}
