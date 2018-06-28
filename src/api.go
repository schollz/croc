package croc

import "time"

type Croc struct {
	TcpPorts            []string
	ServerPort          string
	Timeout             time.Duration
	UseEncryption       bool
	UseCompression      bool
	CurveType           string
	AllowLocalDiscovery bool
}

// Init will initialize the croc relay
func Init() (c *Croc) {
	c = new(Croc)
	c.TcpPorts = []string{"27001", "27002", "27003", "27004"}
	c.ServerPort = "8003"
	c.Timeout = 10 * time.Minute
	c.UseEncryption = true
	c.UseCompression = true
	c.AllowLocalDiscovery = true
	c.CurveType = "p521"
	return
}

// Relay initiates a relay
func (c *Croc) Relay() error {
	// start relay
	go startRelay(c.TcpPorts)

	// start server
	return startServer(c.TcpPorts, c.ServerPort)
}

// Send will take an existing file or folder and send it through the croc relay
func (c *Croc) Send(fname string) (err error) {

	return
}

// Receive will receive something through the croc relay
func (c *Croc) Receive() (err error) {

	return
}
