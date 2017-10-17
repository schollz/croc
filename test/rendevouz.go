package main

import (
	"net"
)

// also see : http://archive.is/4Um4u

// func runRendevouz() {
// 	// Listen on the specified TCP port on all interfaces.
// 	from := "0.0.0.0:27001"
// 	to := "0.0.0.0:27009"
// 	l, err := net.Listen("tcp", to)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer l.Close()
// 	for {
// 		// Wait for a connection.
// 		c, err := l.Accept()
// 		if err != nil {
// 			log.Fatal(err)
// 		}

// 		// handle the connection in a goroutine
// 		go wormhole(c, from)
// 	}
// }

// func relay(c net.Conn, from string) {
// 	defer c.Close()
// 	log.Println("Opening relay to", c.RemoteAddr())

// 	// connect to the destination tcp port
// 	destConn, err := net.Dial("tcp", *to)
// 	if err != nil {
// 		log.Fatal("Error connecting to destination port")
// 	}
// 	defer destConn.Close()
// 	log.Println("Wormhole open from", c.RemoteAddr())

// 	go func() { io.Copy(c, destConn) }()
// 	io.Copy(destConn, c)

// 	log.Println("Stopping wormhole from", c.RemoteAddr())
// }

// BETTER?

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, 1024)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				c <- nil
				break
			}
		}
	}()

	return c
}

// Pipe creates a full-duplex pipe between the two sockets and transfers data from one to the other.
func Pipe(conn1 net.Conn, conn2 net.Conn) {
	chan1 := chanFromConn(conn1)
	chan2 := chanFromConn(conn2)

	for {
		select {
		case b1 := <-chan1:
			if b1 == nil {
				return
			} else {
				conn2.Write(b1)
			}
		case b2 := <-chan2:
			if b2 == nil {
				return
			} else {
				conn1.Write(b2)
			}
		}
	}
}
