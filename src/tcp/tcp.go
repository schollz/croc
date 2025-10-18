package tcp

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"

	"github.com/schollz/croc/v10/src/comm"
	"github.com/schollz/croc/v10/src/crypt"
	"github.com/schollz/croc/v10/src/models"
)

type server struct {
	host       string
	port       string
	debugLevel string
	banner     string
	password   string
	rooms      roomMap

	roomCleanupInterval time.Duration
	roomTTL             time.Duration

	stopRoomCleanup chan struct{}
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

const pingRoom = "pinglkasjdlfjsaldjf"

// newDefaultServer initializes a new server, with some default configuration options
func newDefaultServer() *server {
	s := new(server)
	s.roomCleanupInterval = DEFAULT_ROOM_CLEANUP_INTERVAL
	s.roomTTL = DEFAULT_ROOM_TTL
	s.debugLevel = DEFAULT_LOG_LEVEL
	s.stopRoomCleanup = make(chan struct{})
	return s
}

// RunWithOptionsAsync asynchronously starts a TCP listener.
func RunWithOptionsAsync(host, port, password string, opts ...serverOptsFunc) error {
	s := newDefaultServer()
	s.host = host
	s.port = port
	s.password = password
	for _, opt := range opts {
		err := opt(s)
		if err != nil {
			return fmt.Errorf("could not apply optional configurations: %w", err)
		}
	}
	return s.start()
}

// Run starts a tcp listener, run async
func Run(debugLevel, host, port, password string, banner ...string) (err error) {
	return RunWithOptionsAsync(host, port, password, WithBanner(banner...), WithLogLevel(debugLevel))
}

func (s *server) start() (err error) {
	log.SetLevel(s.debugLevel)

	// Mask our password in logs
	maskedPassword := ""
	if len(s.password) > 2 {
		maskedPassword = fmt.Sprintf("%c***%c", s.password[0], s.password[len(s.password)-1])
	} else {
		maskedPassword = s.password
	}

	log.Debugf("starting with password '%s'", maskedPassword)

	s.rooms.Lock()
	s.rooms.rooms = make(map[string]roomInfo)
	s.rooms.Unlock()

	go s.deleteOldRooms()
	defer s.stopRoomDeletion()

	err = s.run()
	if err != nil {
		log.Error(err)
	}
	return
}

func (s *server) run() (err error) {
	network := "tcp"
	addr := net.JoinHostPort(s.host, s.port)
	if s.host != "" {
		ip := net.ParseIP(s.host)
		if ip == nil {
			var tcpIP *net.IPAddr
			tcpIP, err = net.ResolveIPAddr("ip", s.host)
			if err != nil {
				return err
			}
			ip = tcpIP.IP
		}
		addr = net.JoinHostPort(ip.String(), s.port)
		if s.host != "" {
			if ip.To4() != nil {
				network = "tcp4"
			} else {
				network = "tcp6"
			}
		}
	}
	addr = strings.Replace(addr, "127.0.0.1", "0.0.0.0", 1)
	log.Info("starting TCP server on " + addr)
	server, err := net.Listen(network, addr)
	if err != nil {
		return fmt.Errorf("error listening on %s: %w", addr, err)
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

// deleteOldRooms checks for rooms at a regular interval and removes those that
// have exceeded their allocated TTL.
func (s *server) deleteOldRooms() {
	ticker := time.NewTicker(s.roomCleanupInterval)
	for {
		select {
		case <-ticker.C:
			var roomsToDelete []string
			s.rooms.Lock()
			for room := range s.rooms.rooms {
				if time.Since(s.rooms.rooms[room].opened) > s.roomTTL {
					roomsToDelete = append(roomsToDelete, room)
				}
			}
			s.rooms.Unlock()

			for _, room := range roomsToDelete {
				s.deleteRoom(room)
				log.Debugf("room cleaned up: %s", room)
			}
		case <-s.stopRoomCleanup:
			ticker.Stop()
			log.Debug("room cleanup stopped")
			return
		}
	}
}

func (s *server) stopRoomDeletion() {
	log.Debug("stop room cleanup fired")
	s.stopRoomCleanup <- struct{}{}
}

var weakKey = []byte{1, 2, 3}

func (s *server) clientCommunication(port string, c *comm.Comm) (room string, err error) {
	// establish secure password with PAKE for communication with relay
	B, err := pake.InitCurve(weakKey, 1, "siec")
	if err != nil {
		return
	}
	Abytes, err := c.Receive()
	if err != nil {
		return
	}
	log.Debugf("Abytes: %s", Abytes)
	if bytes.Equal(Abytes, []byte("ping")) {
		room = pingRoom
		log.Debug("sending back pong")
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
		enc, _ := crypt.Encrypt([]byte(err.Error()), strongKeyForEncryption)
		if err = c.Send(enc); err != nil {
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
//
//	Read()s from the socket to the channel.
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
	log.Debugf("pinging %s", address)
	c, err := comm.NewConnection(address, 300*time.Millisecond)
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send([]byte("ping"))
	if err != nil {
		log.Debug(err)
		return
	}
	b, err := c.Receive()
	if err != nil {
		log.Debug(err)
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
		log.Debug(err)
		return
	}

	// get PAKE connection with server to establish strong key to transfer info
	A, err := pake.InitCurve(weakKey, 0, "siec")
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send(A.Bytes())
	if err != nil {
		log.Debug(err)
		return
	}
	Bbytes, err := c.Receive()
	if err != nil {
		log.Debug(err)
		return
	}
	err = A.Update(Bbytes)
	if err != nil {
		log.Debug(err)
		return
	}
	strongKey, err := A.SessionKey()
	if err != nil {
		log.Debug(err)
		return
	}
	log.Debugf("strong key: %x", strongKey)

	strongKeyForEncryption, salt, err := crypt.New(strongKey, nil)
	if err != nil {
		log.Debug(err)
		return
	}
	// send salt
	err = c.Send(salt)
	if err != nil {
		log.Debug(err)
		return
	}

	log.Debug("sending password")
	bSend, err := crypt.Encrypt([]byte(password), strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send(bSend)
	if err != nil {
		log.Debug(err)
		return
	}
	log.Debug("waiting for first ok")
	enc, err := c.Receive()
	if err != nil {
		log.Debug(err)
		return
	}
	data, err := crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	if !strings.Contains(string(data), "|||") {
		err = fmt.Errorf("bad response: %s", string(data))
		log.Debug(err)
		return
	}
	banner = strings.Split(string(data), "|||")[0]
	ipaddr = strings.Split(string(data), "|||")[1]
	log.Debugf("sending room; %s", room)
	bSend, err = crypt.Encrypt([]byte(room), strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send(bSend)
	if err != nil {
		log.Debug(err)
		return
	}
	log.Debug("waiting for room confirmation")
	enc, err = c.Receive()
	if err != nil {
		log.Debug(err)
		return
	}
	data, err = crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	if !bytes.Equal(data, []byte("ok")) {
		err = fmt.Errorf("got bad response: %s", data)
		log.Debug(err)
		return
	}
	log.Debug("all set")
	return
}
