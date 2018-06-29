package croc

// Relay initiates a relay
func (c *Croc) Relay() error {
	// start relay
	go c.startRelay(c.TcpPorts)

	// start server
	return c.startServer(c.TcpPorts, c.ServerPort)
}

// Send will take an existing file or folder and send it through the croc relay
func (c *Croc) Send(fname string) (err error) {
	err = c.client(0)
	return
}

// Receive will receive something through the croc relay
func (c *Croc) Receive() (err error) {
	err = c.client(1)
	return
}
