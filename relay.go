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

const MAX_NUMBER_THREADS = 8

type connectionMap struct {
	reciever map[string]net.Conn
	sender   map[string]net.Conn
	metadata map[string]string
	sync.RWMutex
}

type Relay struct {
	connections         connectionMap
	Debug               bool
	NumberOfConnections int
}

func NewRelay(flags *Flags) *Relay {
	r := new(Relay)
	r.Debug = flags.Debug
	r.NumberOfConnections = MAX_NUMBER_THREADS
	log.SetFormatter(&log.TextFormatter{})
	if r.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}
	return r
}

func (r *Relay) Run() {
	r.connections = connectionMap{}
	r.connections.Lock()
	r.connections.reciever = make(map[string]net.Conn)
	r.connections.sender = make(map[string]net.Conn)
	r.connections.metadata = make(map[string]string)
	r.connections.Unlock()
	r.runServer()
}

func (r *Relay) runServer() {
	logger := log.WithFields(log.Fields{
		"function": "main",
	})
	logger.Debug("Initializing")
	var wg sync.WaitGroup
	wg.Add(r.NumberOfConnections)
	for id := 0; id < r.NumberOfConnections; id++ {
		go r.listenerThread(id, &wg)
	}
	wg.Wait()
}

func (r *Relay) listenerThread(id int, wg *sync.WaitGroup) {
	logger := log.WithFields(log.Fields{
		"function": "listenerThread:" + strconv.Itoa(27000+id),
	})

	defer wg.Done()
	err := r.listener(id)
	if err != nil {
		logger.Error(err)
	}
}

func (r *Relay) listener(id int) (err error) {
	port := strconv.Itoa(27001 + id)
	logger := log.WithFields(log.Fields{
		"function": "listener" + ":" + port,
	})
	server, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on "+":"+port)
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
		go r.clientCommuncation(id, connection)
	}
}

func (r *Relay) clientCommuncation(id int, connection net.Conn) {
	sendMessage("who?", connection)

	m := strings.Split(receiveMessage(connection), ".")
	connectionType, codePhrase, metaData := m[0], m[1], m[2]
	key := codePhrase + "-" + strconv.Itoa(id)
	logger := log.WithFields(log.Fields{
		"id":         id,
		"codePhrase": codePhrase,
	})

	if connectionType == "s" {
		logger.Debug("got sender")
		r.connections.Lock()
		r.connections.metadata[key] = metaData
		r.connections.sender[key] = connection
		r.connections.Unlock()
		// wait for receiver
		receiversAddress := ""
		for {
			r.connections.RLock()
			if _, ok := r.connections.reciever[key]; ok {
				receiversAddress = r.connections.reciever[key].RemoteAddr().String()
				logger.Debug("got reciever")
				r.connections.RUnlock()
				break
			}
			r.connections.RUnlock()
			time.Sleep(100 * time.Millisecond)
		}
		logger.Debug("telling sender ok")
		sendMessage(receiversAddress, connection)
		logger.Debug("preparing pipe")
		r.connections.Lock()
		con1 := r.connections.sender[key]
		con2 := r.connections.reciever[key]
		r.connections.Unlock()
		logger.Debug("piping connections")
		Pipe(con1, con2)
		logger.Debug("done piping")
		r.connections.Lock()
		delete(r.connections.sender, key)
		delete(r.connections.reciever, key)
		delete(r.connections.metadata, key)
		r.connections.Unlock()
		logger.Debug("deleted sender and receiver")
	} else {
		// wait for sender's metadata
		sendersAddress := ""
		for {
			r.connections.RLock()
			if _, ok := r.connections.metadata[key]; ok {
				if _, ok2 := r.connections.sender[key]; ok2 {
					sendersAddress = r.connections.sender[key].RemoteAddr().String()
					logger.Debug("got sender meta data")
					r.connections.RUnlock()
					break
				}
			}
			r.connections.RUnlock()
                        if connectionType == "c" {
		                sendMessage("0-0-0-0.0.0.0", connection)
                                return
                        }
			time.Sleep(100 * time.Millisecond)
		}
		// send  meta data
		r.connections.RLock()
		sendMessage(r.connections.metadata[key]+"-"+sendersAddress, connection)
		r.connections.RUnlock()
		// check for receiver's consent
		consent := receiveMessage(connection)
		logger.Debugf("consent: %s", consent)
		if consent == "ok" {
			logger.Debug("got consent")
			r.connections.Lock()
			r.connections.reciever[key] = connection
			r.connections.Unlock()
		}
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
