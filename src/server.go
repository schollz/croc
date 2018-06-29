package croc

import (
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/cihub/seelog"
	"github.com/frankenbeanies/uuid4"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

func (c *Croc) updateChannel(p payloadChannel) (r response, err error) {
	c.rs.Lock()
	defer c.rs.Unlock()
	r.Success = true

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

	// check if the action is to close the channel
	if p.Close {
		delete(c.rs.channel, p.Channel)
		r.Message = "deleted " + p.Channel
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

	// return the current state
	r.Data = c.rs.channel[p.Channel]

	r.Message = fmt.Sprintf("assigned %d keys: %v", len(assignedKeys), assignedKeys)
	return
}

func (c *Croc) joinChannel(p payloadChannel) (r response, err error) {
	c.rs.Lock()
	defer c.rs.Unlock()
	r.Success = true

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
	r.Channel = p.Channel
	if _, ok := c.rs.channel[r.Channel]; !ok {
		c.rs.channel[r.Channel] = newChannelData(r.Channel)
	}

	// assign UUID for the role in the channel
	c.rs.channel[r.Channel].uuids[p.Role] = uuid4.New().String()
	r.UUID = c.rs.channel[r.Channel].uuids[p.Role]
	log.Debugf("(%s) %s has joined as role %d", r.Channel, r.UUID, p.Role)

	// if channel is not open, set initial parameters
	if !c.rs.channel[r.Channel].isopen {
		c.rs.channel[r.Channel].isopen = true
		c.rs.channel[r.Channel].Ports = tcpPorts
		c.rs.channel[r.Channel].startTime = time.Now()
		switch curve := p.Curve; curve {
		case "p224":
			c.rs.channel[r.Channel].curve = elliptic.P224()
		case "p256":
			c.rs.channel[r.Channel].curve = elliptic.P256()
		case "p384":
			c.rs.channel[r.Channel].curve = elliptic.P384()
		case "p521":
			c.rs.channel[r.Channel].curve = elliptic.P521()
		default:
			// TODO:
			// add SIEC
			p.Curve = "p256"
			c.rs.channel[r.Channel].curve = elliptic.P256()
		}
		log.Debugf("(%s) using curve '%s'", r.Channel, p.Curve)
		c.rs.channel[r.Channel].State["curve"] = []byte(p.Curve)
	}

	r.Message = fmt.Sprintf("assigned role %d in channel '%s'", p.Role, r.Channel)
	return
}

func (c *Croc) startServer(tcpPorts []string, port string) (err error) {
	// start cleanup on dangling channels
	go c.channelCleanup()

	// TODO:
	// insert websockets here
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
		for _, key := range keys {
			if time.Since(c.rs.channel[key].startTime) > maximumWait {
				log.Debugf("channel %s has exceeded time, deleting", key)
				delete(c.rs.channel, key)
			}
		}
		c.rs.Unlock()
		time.Sleep(1 * time.Minute)
	}
}
