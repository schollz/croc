package croc

import (
	"fmt"
	"net"
	"net/http"
	"time"

	log "github.com/cihub/seelog"
	"github.com/frankenbeanies/uuid4"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/schollz/pake"
)

// startServer initiates the server which listens for websocket connections
func (c *Croc) startServer() (err error) {
	// start cleanup on dangling channels
	go c.channelCleanup()

	var upgrader = websocket.Upgrader{} // use default options
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// check if HEAD request
		if r.Method == "HEAD" {
			fmt.Fprintf(w, "ok")
			return
		}
		// incoming websocket request
		log.Debugf("connecting remote addr: %+v", r.RemoteAddr)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Debugf("err in websocket: %s", err.Error())
			fmt.Fprintf(w, "?")
			return
		}
		address := r.RemoteAddr
		if _, ok := r.Header["X-Forwarded-For"]; ok {
			address = r.Header["X-Forwarded-For"][0]
		}
		if _, ok := r.Header["X-Real-Ip"]; ok {
			address = r.Header["X-Real-Ip"][0]
		}
		log.Debugf("ws address: %s", ws.RemoteAddr().String())
		log.Debug("getting lock")
		c.rs.Lock()
		c.rs.ips[ws.RemoteAddr().String()] = address
		c.rs.Unlock()
		log.Debugf("connecting remote addr: %s", address)
		if err != nil {
			log.Error("upgrade:", err)
			return
		}
		defer ws.Close()

		var channel string
		for {
			log.Debug("waiting for next message")
			var cd channelData
			err := ws.ReadJSON(&cd)
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
			channel, err = c.processPayload(ws, cd)
			if err != nil {
				// if error, send the error back and then delete the channel
				log.Warn("problem processing payload %+v: %s", cd, err.Error())
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

func (c *Croc) updateChannel(cd channelData) (err error) {
	c.rs.Lock()
	defer c.rs.Unlock()

	// determine if channel is invalid
	if _, ok := c.rs.channel[cd.Channel]; !ok {
		err = errors.Errorf("channel '%s' does not exist", cd.Channel)
		return
	}

	// determine if UUID is invalid for channel
	if cd.UUID != c.rs.channel[cd.Channel].uuids[0] &&
		cd.UUID != c.rs.channel[cd.Channel].uuids[1] {
		err = errors.Errorf("uuid '%s' is invalid", cd.UUID)
		return
	}

	// update each
	c.rs.channel[cd.Channel].Error = cd.Error
	c.rs.channel[cd.Channel].FileReceived = cd.FileReceived
	c.rs.channel[cd.Channel].EncryptedFileMetaData = cd.EncryptedFileMetaData
	c.rs.channel[cd.Channel].ReadyToRead = cd.ReadyToRead
	if c.rs.channel[cd.Channel].Pake == nil {
		c.rs.channel[cd.Channel].Pake = new(pake.Pake)
	}
	c.rs.channel[cd.Channel].Pake.HkA = cd.Pake.HkA
	c.rs.channel[cd.Channel].Pake.HkB = cd.Pake.HkB
	c.rs.channel[cd.Channel].Pake.Role = cd.Pake.Role
	c.rs.channel[cd.Channel].Pake.Uᵤ = cd.Pake.Uᵤ
	c.rs.channel[cd.Channel].Pake.Uᵥ = cd.Pake.Uᵥ
	c.rs.channel[cd.Channel].Pake.Vᵤ = cd.Pake.Vᵤ
	c.rs.channel[cd.Channel].Pake.Vᵥ = cd.Pake.Vᵥ
	c.rs.channel[cd.Channel].Pake.Xᵤ = cd.Pake.Xᵤ
	c.rs.channel[cd.Channel].Pake.Xᵥ = cd.Pake.Xᵥ
	c.rs.channel[cd.Channel].Pake.Yᵤ = cd.Pake.Yᵤ
	c.rs.channel[cd.Channel].Pake.Yᵥ = cd.Pake.Yᵥ
	if cd.Addresses[0] != "" {
		c.rs.channel[cd.Channel].Addresses[0] = cd.Addresses[0]
	}
	if cd.Addresses[1] != "" {
		c.rs.channel[cd.Channel].Addresses[1] = cd.Addresses[1]
	}
	return
}

func (c *Croc) joinChannel(ws *websocket.Conn, cd channelData) (channel string, err error) {
	log.Debugf("joining channel %s", ws.RemoteAddr().String())
	c.rs.Lock()
	defer c.rs.Unlock()

	// determine if sender or recipient
	if cd.Role != 0 && cd.Role != 1 {
		err = errors.Errorf("no such role of %d", cd.Role)
		return
	}

	// determine channel
	if cd.Channel == "" {
		// TODO:
		// find an empty channel
		cd.Channel = "chou"
	}
	if _, ok := c.rs.channel[cd.Channel]; ok {
		// channel is not empty
		if c.rs.channel[cd.Channel].uuids[cd.Role] != "" {
			err = errors.Errorf("channel '%s' already occupied by role %d", cd.Channel, cd.Role)
			return
		}
	}
	log.Debug("creating new channel")
	if _, ok := c.rs.channel[cd.Channel]; !ok {
		c.rs.channel[cd.Channel] = new(channelData)
		c.rs.channel[cd.Channel].connection = make(map[string][2]net.Conn)
	}
	channel = cd.Channel

	// assign UUID for the role in the channel
	c.rs.channel[cd.Channel].uuids[cd.Role] = uuid4.New().String()
	log.Debugf("(%s) %s has joined as role %d", cd.Channel, c.rs.channel[cd.Channel].uuids[cd.Role], cd.Role)
	// send Channel+UUID back to the current person
	err = ws.WriteJSON(channelData{
		Channel: cd.Channel,
		UUID:    c.rs.channel[cd.Channel].uuids[cd.Role],
		Role:    cd.Role,
	})
	if err != nil {
		return
	}

	// if channel is not open, set initial parameters
	if !c.rs.channel[cd.Channel].isopen {
		c.rs.channel[cd.Channel].isopen = true
		c.rs.channel[cd.Channel].Ports = c.TcpPorts
		c.rs.channel[cd.Channel].startTime = time.Now()
		c.rs.channel[cd.Channel].Curve = "p256"
	}
	c.rs.channel[cd.Channel].websocketConn[cd.Role] = ws
	// assign the name
	c.rs.channel[cd.Channel].Addresses[cd.Role] = c.rs.ips[ws.RemoteAddr().String()]
	log.Debugf("assigned role %d in channel '%s'", cd.Role, cd.Channel)
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
			delete(c.rs.ips, wsConn.RemoteAddr().String())
		}
	}
	// delete
	delete(c.rs.channel, channel)
}

func (c *Croc) processPayload(ws *websocket.Conn, cd channelData) (channel string, err error) {
	log.Debugf("processing payload from %s", ws.RemoteAddr().String())
	channel = cd.Channel

	// if the request is to close, delete the channel
	if cd.Close {
		log.Debugf("closing channel %s", cd.Channel)
		c.closeChannel(cd.Channel)
		return
	}

	// if request is to Open, try to open
	if cd.Open {
		channel, err = c.joinChannel(ws, cd)
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
	if cd.Update {
		// update
		err = c.updateChannel(cd)
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
	maximumWait := 3 * time.Hour
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
