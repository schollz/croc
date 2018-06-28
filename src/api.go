package croc

import "time"

type Croc struct {
	tcpPorts       []string
	serverPort     string
	timeout        time.Duration
	useEncryption  bool
	useCompression bool
	curveType      string
}

// Init will initialize the croc relay
func Init() (c *Croc) {
	c = new(Croc)
	c.tcpPorts = []string{"27001", "27002", "27003", "27004"}
	c.serverPort = "8003"
	c.timeout = 10 * time.Minute
	c.useEncryption = true
	c.useCompression = true
	c.curveType = "p521"
	return
}

// TODO:
// OptionTimeout
// OptionCurve
// OptionUseEncryption
// OptionUseCompression

// Relay initiates a relay
func (c *Croc) Relay() error {
	// start relay
	go startRelay(c.tcpPorts)

	// start server
	return startServer(c.tcpPorts, c.serverPort)
}

// Send will take an existing file or folder and send it through the croc relay
func (c *Croc) Send(fname string) (err error) {

	return
}

// Receive will receive something through the croc relay
func (c *Croc) Receive() (err error) {

	return
}
