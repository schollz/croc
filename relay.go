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
const CONNECTION_TIMEOUT = time.Hour

type connectionMap struct {
	receiver           map[string]net.Conn
	sender             map[string]net.Conn
	metadata           map[string]string
	potentialReceivers map[string]struct{}
	rpublicKey         map[string]string
	spublicKey         map[string]string
	sconsent           map[string]string
	passphrase         map[string]string
	receiverReady      map[string]bool
	sync.RWMutex
}

func (c *connectionMap) IsSenderConnected(key string) (found bool) {
	c.RLock()
	defer c.RUnlock()
	_, found = c.sender[key]
	return
}

func (c *connectionMap) IsPotentialReceiverConnected(key string) (found bool) {
	c.RLock()
	defer c.RUnlock()
	_, found = c.potentialReceivers[key]
	return
}

type Relay struct {
	connections         connectionMap
	Debug               bool
	NumberOfConnections int
}

func NewRelay(config *AppConfig) *Relay {
	r := &Relay{
		Debug:               config.Debug,
		NumberOfConnections: MAX_NUMBER_THREADS,
	}

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
	r.connections.receiver = make(map[string]net.Conn)
	r.connections.sender = make(map[string]net.Conn)
	r.connections.metadata = make(map[string]string)
	r.connections.spublicKey = make(map[string]string)
	r.connections.rpublicKey = make(map[string]string)
	r.connections.passphrase = make(map[string]string)
	r.connections.sconsent = make(map[string]string)
	r.connections.potentialReceivers = make(map[string]struct{})
	r.connections.receiverReady = make(map[string]bool)
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
	defer wg.Done()

	if err := r.listener(id); err != nil {
		return
	}
}

func (r *Relay) listener(id int) (err error) {
	port := strconv.Itoa(27001 + id)
	logger := log.WithFields(log.Fields{
		"function": "listener:" + port,
	})
	server, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on :"+port)
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
	logger := log.WithFields(log.Fields{
		"id": id,
		"ip": connection.RemoteAddr().String(),
	})

	sendMessage("who?", connection)
	m := strings.Split(receiveMessage(connection), ".")
	if len(m) < 4 {
		logger.Debug("exiting, not enough information")
		sendMessage("not enough information", connection)
		return
	}
	connectionType, publicKey, codePhrase, metaData := m[0], m[1], m[2], m[3]
	logger.Debugf("got connection from %s", publicKey)
	key := codePhrase + "-" + strconv.Itoa(id)

	switch connectionType {
	case "s": // sender connection
		defer func() {
			r.connections.Lock()
			// close connections
			r.connections.sender[key].Close()
			r.connections.receiver[key].Close()
			// delete connctions
			delete(r.connections.sender, key)
			delete(r.connections.receiver, key)
			delete(r.connections.metadata, key)
			delete(r.connections.potentialReceivers, key)
			delete(r.connections.spublicKey, key)
			delete(r.connections.rpublicKey, key)
			delete(r.connections.receiverReady, key)
			delete(r.connections.passphrase, key)
			r.connections.Unlock()
			logger.Debug("deleted sender and receiver")
		}()
		if r.connections.IsSenderConnected(key) {
			sendMessage("no", connection)
			return
		}

		r.connections.Lock()
		r.connections.metadata[key] = metaData
		r.connections.sender[key] = connection
		r.connections.spublicKey[key] = publicKey
		r.connections.Unlock()
		// wait for receiver
		receiversAddress := ""
		receiversPublicKey := ""
		isTimeout := time.Duration(0)
		for {
			if CONNECTION_TIMEOUT <= isTimeout {
				sendMessage("timeout", connection)
				break
			}
			r.connections.RLock()
			if _, ok := r.connections.receiver[key]; ok {
				receiversAddress = r.connections.receiver[key].RemoteAddr().String()
			}
			if _, ok := r.connections.rpublicKey[key]; ok {
				receiversPublicKey = r.connections.rpublicKey[key]
			}
			r.connections.RUnlock()
			if receiversAddress != "" && receiversPublicKey != "" {
				break
			}
			time.Sleep(100 * time.Millisecond)
			isTimeout += 100 * time.Millisecond
		}
		logger.Debug("telling sender ok")
		sendMessage(receiversAddress, connection)
		sendMessage(receiversPublicKey, connection)

		// TODO ASK FOR OKAY HERE TOO
		sconsent := receiveMessage(connection)
		r.connections.Lock()
		r.connections.sconsent[key] = sconsent
		r.connections.Unlock()
		logger.Debugf("got consent: %+v", sconsent)
		if sconsent != "ok" {
			return
		}

		logger.Debug("waiting for encrypted passphrase")
		encryptedPassphrase := receiveMessage(connection)
		r.connections.Lock()
		r.connections.passphrase[key] = encryptedPassphrase
		r.connections.Unlock()

		// wait for receiver ready
		for {
			r.connections.RLock()
			if _, ok := r.connections.receiverReady[key]; ok {
				r.connections.RUnlock()
				break
			}
			r.connections.RUnlock()
		}
		// go reciever ready tell sender to go
		sendMessage("go", connection)
		logger.Debug("preparing pipe")
		r.connections.Lock()
		con1 := r.connections.sender[key]
		con2 := r.connections.receiver[key]
		r.connections.Unlock()
		logger.Debug("piping connections")
		Pipe(con1, con2)
		logger.Debug("done piping")
	case "r", "c": // receiver
		if r.connections.IsPotentialReceiverConnected(key) {
			sendMessage("no", connection)
			return
		}

		// add as a potential receiver
		r.connections.Lock()
		r.connections.potentialReceivers[key] = struct{}{}
		r.connections.rpublicKey[key] = publicKey
		r.connections.receiver[key] = connection
		r.connections.Unlock()
		// wait for sender's metadata
		sendersAddress := ""
		sendersPublicKey := ""
		for {
			r.connections.RLock()
			if _, ok := r.connections.metadata[key]; ok {
				if _, ok2 := r.connections.sender[key]; ok2 {
					sendersAddress = r.connections.sender[key].RemoteAddr().String()
					logger.Debug("got sender meta data")
				}
			}
			if _, ok := r.connections.spublicKey[key]; ok {
				sendersPublicKey = r.connections.spublicKey[key]
				logger.Debugf("got sender public key: %s", sendersPublicKey)
			}
			r.connections.RUnlock()
			if sendersAddress != "" && sendersPublicKey != "" {
				break
			}
			if connectionType == "c" {
				sendMessage("0-0-0-0.0.0.0", connection)
				// sender is not ready so delete connection
				r.connections.Lock()
				delete(r.connections.potentialReceivers, key)
				r.connections.Unlock()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		// send  meta data
		r.connections.RLock()
		sendMessage(r.connections.metadata[key]+"-"+sendersAddress, connection)
		sendMessage(sendersPublicKey, connection)
		r.connections.RUnlock()

		// check for senders consent
		sendersConsent := ""
		for {
			r.connections.RLock()
			if _, ok := r.connections.sconsent[key]; ok {
				sendersConsent = r.connections.sconsent[key]
				logger.Debugf("got sender passphrase: %s", sendersConsent)
			}
			r.connections.RUnlock()
			if sendersConsent != "" {
				break
			}
		}
		if sendersConsent != "ok" {
			// TODO: delete everything
			return
		}

		// now get passphrase
		sendersPassphrase := ""
		for {
			r.connections.RLock()
			if _, ok := r.connections.passphrase[key]; ok {
				sendersPassphrase = r.connections.passphrase[key]
				logger.Debugf("got sender passphrase: %s", sendersPassphrase)
			}
			r.connections.RUnlock()
			if sendersPassphrase != "" {
				break
			}
		}

		// check for receiver's consent
		consent := receiveMessage(connection)
		logger.Debugf("consent: %s", consent)
		if consent == "ok" {
			logger.Debug("got consent")
			// wait for encrypted passphrase
			encryptedPassphrase := ""
			for {
				r.connections.RLock()
				if _, ok := r.connections.passphrase[key]; ok {
					encryptedPassphrase = r.connections.passphrase[key]
					logger.Debugf("got passphrase: %s", r.connections.passphrase[key])
				}
				r.connections.RUnlock()
				if encryptedPassphrase != "" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			sendMessage(encryptedPassphrase, connection)
		}
		receiveMessage(connection)
		time.Sleep(10 * time.Millisecond)
		r.connections.Lock()
		r.connections.receiverReady[key] = true
		r.connections.Unlock()
	default:
		logger.Debugf("Got unknown protocol: '%s'", connectionType)
	}
}

func sendMessage(message string, connection net.Conn) {
	message = fillString(message, BUFFERSIZE)
	connection.Write([]byte(message))
}

func receiveMessage(connection net.Conn) string {
	logger := log.WithFields(log.Fields{
		"func": "receiveMessage",
		"ip":   connection.RemoteAddr().String(),
	})
	messageByte := make([]byte, BUFFERSIZE)
	err := connection.SetReadDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		logger.Warn(err)
	}
	err = connection.SetDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		logger.Warn(err)
	}
	err = connection.SetWriteDeadline(time.Now().Add(60 * time.Minute))
	if err != nil {
		logger.Warn(err)
	}
	_, err = connection.Read(messageByte)
	if err != nil {
		logger.Debug(err)
		logger.Debug("no response")
		return ""
	}
	return strings.TrimRight(string(messageByte), ":")
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
