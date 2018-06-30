package croc

func init() {
	SetLogLevel("debug")
}

// Relay initiates a relay
func (c *Croc) Relay() error {
	// start relay
	go c.startRelay(c.TcpPorts)

	// start server
	return c.startServer(c.TcpPorts, c.ServerPort)
}

// Send will take an existing file or folder and send it through the croc relay
func (c *Croc) Send(fname string, codephrase string) (err error) {
	return c.client(0, codephrase, fname)
}

// Receive will receive something through the croc relay
func (c *Croc) Receive(codephrase string) (err error) {
	return c.client(1, codephrase)
}
