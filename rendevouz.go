package main

import (
	"flag"
	"io"
	"log"
	"net"
)

var from *string
var to *string

func init() {
	from = flag.String("from", "0.0.0.0:443", "The address and port that wormhole should listen on.  Connections enter here.")
	to = flag.String("to", "127.0.0.1:80", "Specifies the address and port that wormhole should redirect TCP connections to.  Connections exit here.")
	flag.Parse()
}

func main() {

	// Listen on the specified TCP port on all interfaces.
	l, err := net.Listen("tcp", *from)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	for {
		// Wait for a connection.
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		// handle the connection in a goroutine
		go wormhole(c)
	}
}

// wormhole opens a wormhole from the client connection
// to user the specified destination
func wormhole(c net.Conn) {
	defer c.Close()
	log.Println("Opening wormhole from", c.RemoteAddr())

	// connect to the destination tcp port
	destConn, err := net.Dial("tcp", *to)
	if err != nil {
		log.Fatal("Error connecting to destination port")
	}
	defer destConn.Close()
	log.Println("Wormhole open from", c.RemoteAddr())

	go func() { io.Copy(c, destConn) }()
	io.Copy(destConn, c)

	log.Println("Stopping wormhole from", c.RemoteAddr())
}
