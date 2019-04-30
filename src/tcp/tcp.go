package tcp

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
	"github.com/schollz/croc/v6/src/comm"
	"github.com/schollz/croc/v6/src/logger"
	"github.com/schollz/croc/v6/src/models"
)

type server struct {
	port       string
	debugLevel string
	banner     string
	rooms      roomMap
}

type roomInfo struct {
	first  *comm.Comm
	second *comm.Comm
	opened time.Time
	full   bool
}

type roomMap struct {
	rooms map[string]roomInfo
	sync.Mutex
}

// Run starts a tcp listener, run async
func Run(debugLevel, port string, banner ...string) (err error) {
	s := new(server)
	s.port = port
	s.debugLevel = debugLevel
	if len(banner) > 0 {
		s.banner = banner[0]
	}
	return s.start()
}

func (s *server) start() (err error) {
	logger.SetLogLevel(s.debugLevel)
	s.rooms.Lock()
	s.rooms.rooms = make(map[string]roomInfo)
	s.rooms.Unlock()

	// delete old rooms
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			s.rooms.Lock()
			for room := range s.rooms.rooms {
				if time.Since(s.rooms.rooms[room].opened) > 3*time.Hour {
					delete(s.rooms.rooms, room)
				}
			}
			s.rooms.Unlock()
		}
	}()

	err = s.run()
	if err != nil {
		log.Error(err)
	}
	return
}

func (s *server) run() (err error) {
	log.Debugf("starting TCP server on " + s.port)
	server, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return errors.Wrap(err, "Error listening on :"+s.port)
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
			errCommunication := s.clientCommuncation(port, comm.New(connection))
			if errCommunication != nil {
				log.Warnf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
			}
		}(s.port, connection)
	}
}

func (s *server) clientCommuncation(port string, c *comm.Comm) (err error) {
	// send ok to tell client they are connected
	log.Debug("sending ok")
	err = c.Send([]byte(s.banner))
	if err != nil {
		return
	}

	// wait for client to tell me which room they want
	log.Debug("waiting for answer")
	roomBytes, err := c.Receive()
	if err != nil {
		return
	}
	room := string(roomBytes)

	s.rooms.Lock()
	// create the room if it is new
	if _, ok := s.rooms.rooms[room]; !ok {
		s.rooms.rooms[room] = roomInfo{
			first:  c,
			opened: time.Now(),
		}
		s.rooms.Unlock()
		// tell the client that they got the room
		err = c.Send([]byte("ok"))
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("room %s has 1", room)
		return nil
	}
	if s.rooms.rooms[room].full {
		s.rooms.Unlock()
		err = c.Send([]byte("room full"))
		if err != nil {
			log.Error(err)
			return
		}
		return nil
	}
	log.Debugf("room %s has 2", room)
	s.rooms.rooms[room] = roomInfo{
		first:  s.rooms.rooms[room].first,
		second: c,
		opened: s.rooms.rooms[room].opened,
		full:   true,
	}
	otherConnection := s.rooms.rooms[room].first
	s.rooms.Unlock()

	// second connection is the sender, time to staple connections
	var wg sync.WaitGroup
	wg.Add(1)

	// start piping
	go func(com1, com2 *comm.Comm, wg *sync.WaitGroup) {
		log.Debug("starting pipes")
		pipe(com1.Connection(), com2.Connection())
		wg.Done()
		log.Debug("done piping")
	}(otherConnection, c, &wg)

	// tell the sender everything is ready
	err = c.Send([]byte("ok"))
	if err != nil {
		return
	}
	wg.Wait()

	// delete room
	s.rooms.Lock()
	log.Debugf("deleting room: %s", room)
	s.rooms.rooms[room].first.Close()
	s.rooms.rooms[room].second.Close()
	s.rooms.rooms[room] = roomInfo{first: nil, second: nil}
	delete(s.rooms.rooms, room)
	s.rooms.Unlock()
	return nil
}

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte, 1)

	go func() {
		b := make([]byte, models.TCP_BUFFER_SIZE)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				log.Debug(err)
				c <- nil
				break
			}
		}
		log.Debug("exiting")
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

func ConnectToTCPServer(address, room string) (c *comm.Comm, banner string, err error) {
	c, err = comm.NewConnection(address)
	if err != nil {
		return
	}
	data, err := c.Receive()
	if err != nil {
		return
	}
	banner = string(data)
	err = c.Send([]byte(room))
	if err != nil {
		return
	}
	data, err = c.Receive()
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte("ok")) {
		err = fmt.Errorf("got bad response: %s", data)
		return
	}
	return
}
