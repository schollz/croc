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
	log.Debugf("sending %s with compression, encryption: (%v, %v)", fname, c.UseCompression, c.UseEncryption)
	// prepare code phrase
	defer c.cleanup()
	c.cs.Lock()
	c.cs.channel.codePhrase = codePhrase
	if len(codePhrase) == 0 {
		// generate code phrase
		codePhrase = getRandomName()
	}
	if len(codePhrase) < 4 {
		err = errors.New("code phrase must be more than 4 characters")
		c.cs.Unlock()
		return
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
		if c.NoLocal {
			return
		}
		log.Debug("listening for local croc relay...")
		go peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     1,
			TimeLimit: 600 * time.Second,
			Delay:     50 * time.Millisecond,
			Payload:   []byte(codePhrase[:3]),
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
	type runInfo struct {
		err           error
		bothConnected bool
	}
	runClientError := make(chan runInfo, 2)
	go func() {
		if c.NoLocal {
			return
		}
		d := Init()
		d.ServerPort = "8140"
		d.TcpPorts = []string{"27140", "27141"}
		go d.startRelay()
		go d.startServer()
		time.Sleep(100 * time.Millisecond)
		ce := Init()
		ce.WebsocketAddress = "ws://127.0.0.1:8140"
		// copy over the information
		c.cs.Lock()
		ce.cs.Lock()
		ce.cs.channel.codePhrase = codePhrase
		ce.cs.channel.Channel = codePhrase[:3]
		ce.cs.channel.passPhrase = codePhrase[3:]
		ce.cs.channel.fileMetaData = c.cs.channel.fileMetaData
		ce.crocFile = c.crocFile
		ce.crocFileEncrypted = ce.crocFileEncrypted
		ce.isLocal = true
		ce.cs.Unlock()
		c.cs.Unlock()
		defer func() {
			// delete croc files
			ce.cleanup()
		}()
		var ri runInfo
		ri.err = ce.client(0, channel)
		ri.bothConnected = ce.bothConnected
		runClientError <- ri
	}()

	// start main client
	go func() {
		if c.LocalOnly {
			return
		}
		var ri runInfo
		ri.err = c.client(0, channel)
		ri.bothConnected = c.bothConnected
		runClientError <- ri
	}()

	var ri runInfo
	ri = <-runClientError
	if ri.bothConnected || c.LocalOnly || c.NoLocal {
		return ri.err
	}
	ri = <-runClientError
	return ri.err
}

// Receive will receive something through the croc relay
func (c *Croc) Receive(codePhrase string) (err error) {
	defer c.cleanup()
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
			log.Debugf("discovered %s on %s", discovered[0].Payload, discovered[0].Address)
			_, connectTimeout := net.DialTimeout("tcp", discovered[0].Address+":27140", 1*time.Second)
			if connectTimeout == nil {
				log.Debug("connected")
				c.WebsocketAddress = "ws://" + discovered[0].Address + ":8140"
				c.isLocal = true
				log.Debug(discovered[0].Address)
				// codePhrase = string(discovered[0].Payload)
			} else {
				log.Debug("but could not connect to ports")
			}
		} else {
			log.Debug("discovered no peers")
		}
	}

	// prepare codephrase
	c.cs.Lock()
	if len(codePhrase) == 0 {
		// prompt codephrase
		codePhrase = promptCodePhrase()
	}
	if len(codePhrase) < 4 {
		err = errors.New("code phrase must be more than 4 characters")
		c.cs.Unlock()
		return
	}
	c.cs.channel.codePhrase = codePhrase
	c.cs.channel.Channel = codePhrase[:3]
	c.cs.channel.passPhrase = codePhrase[3:]
	log.Debugf("codephrase: '%s'", codePhrase)
	log.Debugf("channel: '%s'", c.cs.channel.Channel)
	log.Debugf("passPhrase: '%s'", c.cs.channel.passPhrase)
	channel := c.cs.channel.Channel
	c.cs.Unlock()

	return c.client(1, channel)
}
