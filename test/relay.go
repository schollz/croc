package main

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type connectionMap struct {
	reciever map[string]net.Conn
	sender   map[string]net.Conn
	sync.RWMutex
}

var connections connectionMap

func init() {
	connections.Lock()
	connections.reciever = make(map[string]net.Conn)
	connections.sender = make(map[string]net.Conn)
	connections.Unlock()
}

func runServer() {
	logger := log.WithFields(log.Fields{
		"function": "main",
	})
	logger.Debug("Initializing")
	var wg sync.WaitGroup
	wg.Add(numberConnections)
	for id := 0; id < numberConnections; id++ {
		go listenerThread(id, &wg)
	}
	wg.Wait()
}

func listenerThread(id int, wg *sync.WaitGroup) {
	logger := log.WithFields(log.Fields{
		"function": "listenerThread@" + serverAddress + ":" + strconv.Itoa(27000+id),
	})

	defer wg.Done()
	err := listener(id)
	if err != nil {
		logger.Error(err)
	}
}

func listener(id int) (err error) {
	port := strconv.Itoa(27001 + id)
	logger := log.WithFields(log.Fields{
		"function": "listener@" + serverAddress + ":" + port,
	})
	server, err := net.Listen("tcp", serverAddress+":"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on "+serverAddress+":"+port)
	}
	defer server.Close()
	logger.Debug("waiting for connections")
	//Spawn a new goroutine whenever a client connects
	for {
		connection, err := server.Accept()
		if err != nil {
			return errors.Wrap(err, "problem accepting connection")
		}
		logger.Debugf("Client %s connected", connection.RemoteAddr().String())
		go clientCommuncation(id, connection)
	}
}

func clientCommuncation(id int, connection net.Conn) {
	sendMessage("who?", connection)
	message := receiveMessage(connection)
	connectionType := strings.Split(message, ".")[0]
	codePhrase := strings.Split(message, ".")[1] + "-" + strconv.Itoa(id)
	logger := log.WithFields(log.Fields{
		"id":         id,
		"codePhrase": codePhrase,
	})

	if connectionType == "s" {
		logger.Debug("got sender")
		connections.Lock()
		connections.sender[codePhrase] = connection
		connections.Unlock()
		for {
			connections.RLock()
			if _, ok := connections.reciever[codePhrase]; ok {
				logger.Debug("got reciever")
				connections.RUnlock()
				break
			}
			connections.RUnlock()
			time.Sleep(100 * time.Millisecond)
		}
		logger.Debug("telling sender ok")
		sendMessage("ok", connection)
		logger.Debug("preparing pipe")
		connections.Lock()
		con1 := connections.sender[codePhrase]
		con2 := connections.reciever[codePhrase]
		connections.Unlock()
		logger.Debug("piping connections")
		Pipe(con1, con2)
		logger.Debug("done piping")
		connections.Lock()
		delete(connections.sender, codePhrase)
		delete(connections.reciever, codePhrase)
		connections.Unlock()
		logger.Debug("deleted sender and receiver")
	} else {
		logger.Debug("got reciever")
		connections.Lock()
		connections.reciever[codePhrase] = connection
		connections.Unlock()
	}
	return
}

func sendMessage(message string, connection net.Conn) {
	message = fillString(message, BUFFERSIZE)
	connection.Write([]byte(message))
}

func receiveMessage(connection net.Conn) string {
	messageByte := make([]byte, BUFFERSIZE)
	connection.Read(messageByte)
	return strings.Replace(string(messageByte), ":", "", -1)
}

func fillString(retunString string, toLength int) string {
	for {
		lengthString := len(retunString)
		if lengthString < toLength {
			retunString = retunString + ":"
			continue
		}
		break
	}
	return retunString
}

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, BUFFERSIZE)

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
