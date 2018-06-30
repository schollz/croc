package croc

import (
	"net"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
	"github.com/schollz/peerdiscovery"
)

func init() {
	SetLogLevel("debug")
}

// Relay initiates a relay
func (c *Croc) Relay() error {
	// start relay
	go c.startRelay()

	// start server
	return c.startServer()
}

// Send will take an existing file or folder and send it through the croc relay
func (c *Croc) Send(fname string, codePhrase string) (err error) {
	// prepare code phrase

	c.cs.Lock()
	c.cs.channel.codePhrase = codePhrase
	if len(codePhrase) > 0 {
		if len(codePhrase) < 4 {
			err = errors.New("code phrase must be more than 4 characters")
			c.cs.Unlock()
			return
		}
	} else {
		// generate code phrase
		codePhrase = getRandomName()
	}
	c.cs.channel.codePhrase = codePhrase
	c.cs.channel.Channel = codePhrase[:3]
	c.cs.channel.passPhrase = codePhrase[3:]
	log.Debugf("codephrase: '%s'", codePhrase)
	log.Debugf("channel: '%s'", c.cs.channel.Channel)
	log.Debugf("passPhrase: '%s'", c.cs.channel.passPhrase)
	channel := c.cs.channel.Channel
	c.cs.Unlock()

	// start peer discovery
	go func() {
		log.Debug("listening for local croc relay...")
		go peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     1,
			TimeLimit: 600 * time.Second,
			Delay:     50 * time.Millisecond,
			Payload:   []byte(codePhrase),
		})
	}()

	if len(fname) == 0 {
		err = errors.New("must include filename")
		return
	}
	err = c.processFile(fname)
	if err != nil {
		return
	}

	// start relay for listening
	runClientError := make(chan error)
	go func() {
		d := Init()
		d.ServerPort = "8140"
		d.TcpPorts = []string{"27140", "27141"}
		go d.startRelay()
		go d.startServer()
		e := Init()
		e.WebsocketAddress = "ws://127.0.0.1:8140"
		runClientError <- e.client(0, channel)
	}()

	// start main client
	go func() {
		runClientError <- c.client(0, channel)
	}()
	return <-runClientError
}

// Receive will receive something through the croc relay
func (c *Croc) Receive(codePhrase string) (err error) {
	// prepare codephrase
	c.cs.Lock()
	c.cs.channel.codePhrase = codePhrase
	if len(codePhrase) > 0 {
		if len(codePhrase) < 4 {
			err = errors.New("code phrase must be more than 4 characters")
			c.cs.Unlock()
			return
		}
		c.cs.channel.Channel = codePhrase[:3]
		c.cs.channel.passPhrase = codePhrase[3:]
	} else {
		// prompt codephrase
		codePhrase = promptCodePhrase()
		if len(codePhrase) < 4 {
			err = errors.New("code phrase must be more than 4 characters")
			c.cs.Unlock()
			return
		}
		c.cs.channel.Channel = codePhrase[:3]
		c.cs.channel.passPhrase = codePhrase[3:]
	}
	log.Debugf("codephrase: '%s'", codePhrase)
	log.Debugf("channel: '%s'", c.cs.channel.Channel)
	log.Debugf("passPhrase: '%s'", c.cs.channel.passPhrase)
	channel := c.cs.channel.Channel
	c.cs.Unlock()

	// try to discovery codephrase and server through peer network
	discovered, errDiscover := peerdiscovery.Discover(peerdiscovery.Settings{
		Limit:     1,
		TimeLimit: 1 * time.Second,
		Delay:     50 * time.Millisecond,
		Payload:   []byte(codePhrase),
	})
	if errDiscover != nil {
		log.Debug(errDiscover)
	}
	if len(discovered) > 0 {
		log.Debugf("discovered %s on %s", discovered[0].Payload, discovered[0].Address)
		_, connectTimeout := net.DialTimeout("tcp", discovered[0].Address+":27140", 1*time.Second)
		if connectTimeout == nil {
			log.Debug("connected")
			c.WebsocketAddress = "ws://" + discovered[0].Address + ":8140"
			log.Debug(discovered[0].Address)
			codePhrase = string(discovered[0].Payload)
		} else {
			log.Debug("but could not connect to ports")
		}
	} else {
		log.Debug("discovered no peers")
	}

	return c.client(1, channel)
}
