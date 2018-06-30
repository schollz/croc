package croc

import (
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

func (c *Croc) startRelay(ports []string) {
	var wg sync.WaitGroup
	wg.Add(len(ports))
	for _, port := range ports {
		go func(port string, wg *sync.WaitGroup) {
			defer wg.Done()
			log.Debugf("listening on port %s", port)
			if err := c.listener(port); err != nil {
				log.Error(err)
				return
			}
		}(port, &wg)
	}
	wg.Wait()
}

func (c *Croc) listener(port string) (err error) {
	server, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on :"+port)
	}
	defer server.Close()
	// spawn a new goroutine whenever a client connects
	for {
		connection, err := server.Accept()
		if err != nil {
			return errors.Wrap(err, "problem accepting connection")
		}
		log.Debugf("client %s connected", connection.RemoteAddr().String())
		go func(port string, connection net.Conn) {
			errCommunication := c.clientCommuncation(port, connection)
			if errCommunication != nil {
				log.Warnf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
			}
		}(port, connection)
	}
}

func (c *Croc) clientCommuncation(port string, connection net.Conn) (err error) {
	var con1, con2 net.Conn

	// get the channel and UUID from the client
	err = sendMessage("channel and uuid?", connection)
	if err != nil {
		return
	}
	channel, err := receiveMessage(connection)
	if err != nil {
		return
	}
	uuid, err := receiveMessage(connection)
	if err != nil {
		return
	}
	log.Debugf("%s connected to port %s on channel %s and uuid %s", connection.RemoteAddr().String(), port, channel, uuid)

	// validate channel and UUID
	c.rs.Lock()
	if _, ok := c.rs.channel[channel]; !ok {
		c.rs.Unlock()
		err = errors.Errorf("channel %s does not exist", channel)
		return
	}
	if uuid != c.rs.channel[channel].uuids[0] &&
		uuid != c.rs.channel[channel].uuids[1] {
		c.rs.Unlock()
		err = errors.Errorf("uuid '%s' is invalid", uuid)
		return
	}
	role := 0
	if uuid == c.rs.channel[channel].uuids[1] {
		role = 1
	}

	if _, ok := c.rs.channel[channel].connection[port]; !ok {
		c.rs.channel[channel].connection[port] = [2]net.Conn{nil, nil}
	}
	con1 = c.rs.channel[channel].connection[port][0]
	con2 = c.rs.channel[channel].connection[port][1]
	if role == 0 {
		con1 = connection
	} else {
		con2 = connection
	}
	log.Debug(c.rs.channel[channel].connection[port])
	c.rs.channel[channel].connection[port] = [2]net.Conn{con1, con2}
	ports := c.rs.channel[channel].Ports
	c.rs.Unlock()

	if con1 != nil && con2 != nil {
		log.Debugf("beginning the piping")
		var wg sync.WaitGroup
		wg.Add(1)

		// start piping
		go func(con1 net.Conn, con2 net.Conn, wg *sync.WaitGroup) {
			pipe(con1, con2)
			wg.Done()
			log.Debug("done piping")
		}(con1, con2, &wg)

		if port == ports[0] {
			// then set transfer ready
			c.rs.Lock()
			c.rs.channel[channel].TransferReady = true
			c.rs.channel[channel].websocketConn[0].WriteJSON(c.rs.channel[channel])
			c.rs.channel[channel].websocketConn[1].WriteJSON(c.rs.channel[channel])
			c.rs.Unlock()
			log.Debugf("sent ready signal")
		}
		wg.Wait()
		log.Debugf("finished transfer")
	}
	log.Debug("finished client communication")
	return
}

func sendMessage(message string, connection net.Conn) (err error) {
	message = fillString(message, bufferSize)
	_, err = connection.Write([]byte(message))
	return
}

func receiveMessage(connection net.Conn) (s string, err error) {
	messageByte := make([]byte, bufferSize)
	err = connection.SetReadDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	err = connection.SetDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	err = connection.SetWriteDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		return
	}
	_, err = connection.Read(messageByte)
	if err != nil {
		return
	}
	s = strings.TrimRight(string(messageByte), ":")
	return
}

func fillString(returnString string, toLength int) string {
	for {
		lengthString := len(returnString)
		if lengthString < toLength {
			returnString = returnString + ":"
			continue
		}
		break
	}
	return returnString
}

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, bufferSize)

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

// pipe creates a full-duplex pipe between the two sockets and
// transfers data from one to the other.
func pipe(conn1 net.Conn, conn2 net.Conn) {
	chan1 := chanFromConn(conn1)
	chan2 := chanFromConn(conn2)

	for {
		select {
		case b1 := <-chan1:
			if b1 == nil {
				return
			}
			conn2.Write(b1)

		case b2 := <-chan2:
			if b2 == nil {
				return
			}
			conn1.Write(b2)
		}
	}
}
