package main

import (
	"io"
	"log"
	"net"
)

// also see : http://archive.is/4Um4u

func runRendevouz() {
	// Listen on the specified TCP port on all interfaces.
	from := "0.0.0.0:27001"
	to := "0.0.0.0:27009"
	l, err := net.Listen("tcp", to)
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
		go wormhole(c, from)
	}
}

func relay(c net.Conn, from string) {
	defer c.Close()
	log.Println("Opening relay to", c.RemoteAddr())

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
