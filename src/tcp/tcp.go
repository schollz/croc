package tcp

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/logger"
	"github.com/schollz/pake/v2"

	"github.com/schollz/croc/v8/src/comm"
	"github.com/schollz/croc/v8/src/crypt"
	"github.com/schollz/croc/v8/src/models"
)

type server struct {
	port       string
	debugLevel string
	banner     string
	password   string
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

var timeToRoomDeletion = 10 * time.Minute
var pingRoom = "pinglkasjdlfjsaldjf"

// Run starts a tcp listener, run async
func Run(debugLevel, port, password string, banner ...string) (err error) {
	s := new(server)
	s.port = port
	s.password = password
	s.debugLevel = debugLevel
	if len(banner) > 0 {
		s.banner = banner[0]
	}
	return s.start()
}

func (s *server) start() (err error) {
	log.SetLevel(s.debugLevel)
	log.Debugf("starting with password '%s'", s.password)
	s.rooms.Lock()
	s.rooms.rooms = make(map[string]roomInfo)
	s.rooms.Unlock()

	// delete old rooms
	go func() {
		for {
			time.Sleep(timeToRoomDeletion)
			var roomsToDelete []string
			s.rooms.Lock()
			for room := range s.rooms.rooms {
				if time.Since(s.rooms.rooms[room].opened) > 3*time.Hour {
					roomsToDelete = append(roomsToDelete, room)
				}
			}
			s.rooms.Unlock()

			for _, room := range roomsToDelete {
				s.deleteRoom(room)
			}
		}
	}()

	err = s.run()
	if err != nil {
		log.Error(err)
	}
	return
}

func (s *server) run() (err error) {
	log.Infof("starting TCP server on " + s.port)
	server, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("error listening on %s: %w", s.port, err)
	}
	defer server.Close()
	// spawn a new goroutine whenever a client connects
	for {
		connection, err := server.Accept()
		if err != nil {
			return fmt.Errorf("problem accepting connection: %w", err)
		}
		log.Debugf("client %s connected", connection.RemoteAddr().String())
		go func(port string, connection net.Conn) {
			c := comm.New(connection)
			room, errCommunication := s.clientCommunication(port, c)
			log.Debugf("room: %+v", room)
			log.Debugf("err: %+v", errCommunication)
			if errCommunication != nil {
				log.Debugf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
				connection.Close()
				return
			}
			if room == pingRoom {
				log.Debugf("got ping")
				connection.Close()
				return
			}
			for {
				// check connection
				log.Debugf("checking connection of room %s for %+v", room, c)
				deleteIt := false
				s.rooms.Lock()
				if _, ok := s.rooms.rooms[room]; !ok {
					log.Debug("room is gone")
					s.rooms.Unlock()
					return
				}
				log.Debugf("room: %+v", s.rooms.rooms[room])
				if s.rooms.rooms[room].first != nil && s.rooms.rooms[room].second != nil {
					log.Debug("rooms ready")
					s.rooms.Unlock()
					break
				} else {
					if s.rooms.rooms[room].first != nil {
						errSend := s.rooms.rooms[room].first.Send([]byte{1})
						if errSend != nil {
							log.Debug(errSend)
							deleteIt = true
						}
					}
				}
				s.rooms.Unlock()
				if deleteIt {
					s.deleteRoom(room)
					break
				}
				time.Sleep(1 * time.Second)
			}
		}(s.port, connection)
	}
}

var weakKey = []byte{1, 2, 3}

func (s *server) clientCommunication(port string, c *comm.Comm) (room string, err error) {
	// establish secure password with PAKE for communication with relay
	B, err := pake.InitCurve(weakKey, 1, "siec", 1*time.Microsecond)
	if err != nil {
		return
	}
	Abytes, err := c.Receive()
	if err != nil {
		return
	}
	if bytes.Equal(Abytes, []byte("ping")) {
		room = pingRoom
		c.Send([]byte("pong"))
		return
	}
	err = B.Update(Abytes)
	if err != nil {
		return
	}
	err = c.Send(B.Bytes())
	if err != nil {
		return
	}
	Abytes, err = c.Receive()
	if err != nil {
		return
	}
	err = B.Update(Abytes)
	if err != nil {
		return
	}
	strongKey, err := B.SessionKey()
	if err != nil {
		return
	}
	log.Debugf("strongkey: %x", strongKey)

	// receive salt
	salt, err := c.Receive()
	if err != nil {
		return
	}
	strongKeyForEncryption, _, err := crypt.New(strongKey, salt)
	if err != nil {
		return
	}

	log.Debugf("waiting for password")
	passwordBytesEnc, err := c.Receive()
	if err != nil {
		return
	}
	passwordBytes, err := crypt.Decrypt(passwordBytesEnc, strongKeyForEncryption)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(passwordBytes)) != s.password {
		err = fmt.Errorf("bad password")
		enc, enc_err := crypt.Encrypt([]byte(err.Error()), strongKeyForEncryption)
		if enc_err != nil {
			return "", enc_err
		}
		if err := c.Send(enc); err != nil {
			return "", fmt.Errorf("send error: %w", err)
		}
		return
	}

	// send ok to tell client they are connected
	banner := s.banner
	if len(banner) == 0 {
		banner = "ok"
	}
	log.Debugf("sending '%s'", banner)
	bSend, err := crypt.Encrypt([]byte(banner+"|||"+c.Connection().RemoteAddr().String()), strongKeyForEncryption)
	if err != nil {
		return
	}
	err = c.Send(bSend)
	if err != nil {
		return
	}

	// wait for client to tell me which room they want
	log.Debug("waiting for answer")
	enc, err := c.Receive()
	if err != nil {
		return
	}
	roomBytes, err := crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		return
	}
	room = string(roomBytes)

	s.rooms.Lock()
	// create the room if it is new
	if _, ok := s.rooms.rooms[room]; !ok {
		s.rooms.rooms[room] = roomInfo{
			first:  c,
			opened: time.Now(),
		}
		s.rooms.Unlock()
		// tell the client that they got the room

		bSend, err = crypt.Encrypt([]byte("ok"), strongKeyForEncryption)
		if err != nil {
			return
		}
		err = c.Send(bSend)
		if err != nil {
			log.Error(err)
			s.deleteRoom(room)
			return
		}
		log.Debugf("room %s has 1", room)
		return
	}
	if s.rooms.rooms[room].full {
		s.rooms.Unlock()
		bSend, err = crypt.Encrypt([]byte("room full"), strongKeyForEncryption)
		if err != nil {
			return
		}
		err = c.Send(bSend)
		if err != nil {
			log.Error(err)
			s.deleteRoom(room)
			return
		}
		return
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
	bSend, err = crypt.Encrypt([]byte("ok"), strongKeyForEncryption)
	if err != nil {
		return
	}
	err = c.Send(bSend)
	if err != nil {
		s.deleteRoom(room)
		return
	}
	wg.Wait()

	// delete room
	s.deleteRoom(room)
	return
}

func (s *server) deleteRoom(room string) {
	s.rooms.Lock()
	defer s.rooms.Unlock()
	if _, ok := s.rooms.rooms[room]; !ok {
		return
	}
	log.Debugf("deleting room: %s", room)
	if s.rooms.rooms[room].first != nil {
		s.rooms.rooms[room].first.Close()
	}
	if s.rooms.rooms[room].second != nil {
		s.rooms.rooms[room].second.Close()
	}
	s.rooms.rooms[room] = roomInfo{first: nil, second: nil}
	delete(s.rooms.rooms, room)

}

// chanFromConn creates a channel from a Conn object, and sends everything it
//  Read()s from the socket to the channel.
func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte, 1)
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		log.Warnf("can't set read deadline: %v", err)
	}

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
			if _, err := conn2.Write(b1); err != nil {
				log.Errorf("write error on channel 1: %v", err)
			}

		case b2 := <-chan2:
			if b2 == nil {
				return
			}
			if _, err := conn1.Write(b2); err != nil {
				log.Errorf("write error on channel 2: %v", err)
			}
		}
	}
}

func PingServer(address string) (err error) {
	c, err := comm.NewConnection(address, 200*time.Millisecond)
	if err != nil {
		return
	}
	err = c.Send([]byte("ping"))
	if err != nil {
		return
	}
	b, err := c.Receive()
	if err != nil {
		return
	}
	if bytes.Equal(b, []byte("pong")) {
		return nil
	}
	return fmt.Errorf("no pong")
}

// ConnectToTCPServer will initiate a new connection
// to the specified address, room with optional time limit
func ConnectToTCPServer(address, password, room string, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, err error) {
	if len(timelimit) > 0 {
		c, err = comm.NewConnection(address, timelimit[0])
	} else {
		c, err = comm.NewConnection(address)
	}
	if err != nil {
		return
	}

	// get PAKE connection with server to establish strong key to transfer info
	A, err := pake.InitCurve(weakKey, 0, "siec", 1*time.Microsecond)
	if err != nil {
		return
	}
	err = c.Send(A.Bytes())
	if err != nil {
		return
	}
	Bbytes, err := c.Receive()
	if err != nil {
		return
	}
	err = A.Update(Bbytes)
	if err != nil {
		return
	}
	err = c.Send(A.Bytes())
	if err != nil {
		return
	}
	strongKey, err := A.SessionKey()
	if err != nil {
		return
	}
	log.Debugf("strong key: %x", strongKey)

	strongKeyForEncryption, salt, err := crypt.New(strongKey, nil)
	if err != nil {
		return
	}
	// send salt
	err = c.Send(salt)
	if err != nil {
		return
	}

	log.Debug("sending password")
	bSend, err := crypt.Encrypt([]byte(password), strongKeyForEncryption)
	if err != nil {
		return
	}
	err = c.Send(bSend)
	if err != nil {
		return
	}
	log.Debug("waiting for first ok")
	enc, err := c.Receive()
	if err != nil {
		return
	}
	data, err := crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		return
	}
	if !strings.Contains(string(data), "|||") {
		err = fmt.Errorf("bad response: %s", string(data))
		return
	}
	banner = strings.Split(string(data), "|||")[0]
	ipaddr = strings.Split(string(data), "|||")[1]
	log.Debug("sending room")
	bSend, err = crypt.Encrypt([]byte(room), strongKeyForEncryption)
	if err != nil {
		return
	}
	err = c.Send(bSend)
	if err != nil {
		return
	}
	log.Debug("waiting for room confirmation")
	enc, err = c.Receive()
	if err != nil {
		return
	}
	data, err = crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte("ok")) {
		err = fmt.Errorf("got bad response: %s", data)
		return
	}
	log.Debug("all set")
	return
}
