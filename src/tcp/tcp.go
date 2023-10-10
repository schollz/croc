package tcp

import (
	"bytes"
	"container/list"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"

	"github.com/schollz/croc/v9/src/comm"
	"github.com/schollz/croc/v9/src/crypt"
	"github.com/schollz/croc/v9/src/models"
)

type server struct {
	host       string
	port       string
	debugLevel string
	banner     string
	password   string
	rooms      roomMap
}

type roomInfo struct {
	first         *comm.Comm
	second        *comm.Comm
	queue         *list.List
	isMainRoom    bool
	maxOccupants  int
	doneTransfers int
	opened        time.Time
	full          bool
}

type roomMap struct {
	rooms     map[string]roomInfo
	roomLocks map[string]*sync.Mutex
	sync.Mutex
}

const pingRoom = "pinglkasjdlfjsaldjf"

var timeToRoomDeletion = 10 * time.Minute

// Run starts a tcp listener, run async
func Run(debugLevel, host, port, password string, banner ...string) (err error) {
	s := new(server)
	s.host = host
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
	s.rooms.roomLocks = make(map[string]*sync.Mutex)
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
	log.Infof("starting TCP server on " + addr)
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

	isSender := false
	log.Debug("wait for client to tell if they want to send or receive")
	enc, err := c.Receive()
	if err != nil {
		return
	}
	data, err := crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte("send")) && !bytes.Equal(data, []byte("receive")) {
		err = fmt.Errorf("got bad response: %s", data)
		return
	} else if bytes.Equal(data, []byte("send")) {
		log.Debug("client wants to send")
		isSender = true
	}

	// wait for client to tell me which room they want
	log.Debug("waiting for room")
	enc, err = c.Receive()
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
	if _, ok := s.rooms.rooms[room]; !ok && isSender {
		log.Debug("Check if this is a main room")
		enc, err = c.Receive()
		if err != nil {
			return
		}
		data, err = crypt.Decrypt(enc, strongKeyForEncryption)
		if err != nil {
			return
		}
		if !bytes.Equal(data, []byte("main")) && !bytes.Equal(data, []byte("secondary")) {
			err = fmt.Errorf("got bad response: %s", data)
			return
		}
		isMainRoom := bytes.Equal(data, []byte("main"))
		log.Debugf("isMainRoom: %v", isMainRoom)

		maxOccupants := 2
		if isMainRoom {
			log.Debug("Wait for maxOccupants")
			enc, err = c.Receive()
			if err != nil {
				return
			}
			data, err = crypt.Decrypt(enc, strongKeyForEncryption)
			if err != nil {
				return
			}

			maxOccupants, err = strconv.Atoi(string(data))
			if err != nil {
				return
			}
			log.Debugf("maxOccupants: %v", maxOccupants)
		}

		s.rooms.rooms[room] = roomInfo{
			first:         c,
			second:        nil,
			isMainRoom:    isMainRoom,
			maxOccupants:  maxOccupants,
			doneTransfers: 0,
			opened:        time.Now(),
		}
		s.rooms.roomLocks[room] = &sync.Mutex{}
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
		if s.rooms.rooms[room].isMainRoom {
			log.Debugf("room %s is full, adding to queue", room)
			queue := s.rooms.rooms[room].queue
			if queue == nil {
				queue = list.New()
			}
			queue.PushBack(c.ID())
			s.rooms.rooms[room] = roomInfo{
				first:         s.rooms.rooms[room].first,
				second:        s.rooms.rooms[room].second,
				opened:        s.rooms.rooms[room].opened,
				maxOccupants:  s.rooms.rooms[room].maxOccupants,
				doneTransfers: s.rooms.rooms[room].doneTransfers,
				queue:         queue,
				full:          true,
			}
			s.rooms.Unlock()

			for {
				s.rooms.roomLocks[room].Lock()

				if s.rooms.rooms[room].doneTransfers >= s.rooms.rooms[room].maxOccupants {
					s.rooms.roomLocks[room].Unlock()
					// tell the client that the sender is no longer available
					bSend, err = crypt.Encrypt([]byte("sender_gone"), strongKeyForEncryption)
					if err != nil {
						return
					}
					err = c.Send(bSend)
					if err != nil {
						log.Error(err)
						return
					}
					return
				} else if s.rooms.rooms[room].second != nil || s.rooms.rooms[room].queue.Front().Value.(string) != c.ID() {
					s.rooms.roomLocks[room].Unlock()
					time.Sleep(1 * time.Second)
					// tell the client that they need to wait
					bSend, err = crypt.Encrypt([]byte("wait"), strongKeyForEncryption)
					if err != nil {
						return
					}
					err = c.Send(bSend)
					if err != nil {
						log.Error(err)
						return
					}
				} else {
					newQueue := s.rooms.rooms[room].queue
					newQueue.Remove(newQueue.Front())
					s.rooms.rooms[room] = roomInfo{
						first:         s.rooms.rooms[room].first,
						second:        c,
						queue:         newQueue,
						isMainRoom:    s.rooms.rooms[room].isMainRoom,
						maxOccupants:  s.rooms.rooms[room].maxOccupants,
						doneTransfers: s.rooms.rooms[room].doneTransfers,
						opened:        s.rooms.rooms[room].opened,
						full:          true,
					}
					break
				}
			}
			// tell the client that they got the room
			bSend, err = crypt.Encrypt([]byte("ok"), strongKeyForEncryption)
			if err != nil {
				return
			}
			err = c.Send(bSend)
			if err != nil {
				log.Error(err)
				return
			}
		} else {
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
	} else {
		log.Debugf("room %s has 2", room)
		s.rooms.rooms[room] = roomInfo{
			first:         s.rooms.rooms[room].first,
			second:        c,
			queue:         s.rooms.rooms[room].queue,
			isMainRoom:    s.rooms.rooms[room].isMainRoom,
			maxOccupants:  s.rooms.rooms[room].maxOccupants,
			doneTransfers: s.rooms.rooms[room].doneTransfers,
			opened:        s.rooms.rooms[room].opened,
			full:          true,
		}
		s.rooms.roomLocks[room].Lock()
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

	newDoneTransfers := s.rooms.rooms[room].doneTransfers + 1
	if newDoneTransfers == s.rooms.rooms[room].maxOccupants {
		log.Debugf("room %s is done", room)
		// delete room
		s.deleteRoom(room)
	} else {
		log.Debugf("room %s has %d done", room, newDoneTransfers)
		s.rooms.Lock()
		lengthOfQueue := 0
		if s.rooms.rooms[room].queue != nil {
			lengthOfQueue = s.rooms.rooms[room].queue.Len()
		}
		s.rooms.rooms[room] = roomInfo{
			first:         s.rooms.rooms[room].first,
			second:        nil,
			queue:         s.rooms.rooms[room].queue,
			isMainRoom:    s.rooms.rooms[room].isMainRoom,
			maxOccupants:  s.rooms.rooms[room].maxOccupants,
			doneTransfers: newDoneTransfers,
			opened:        s.rooms.rooms[room].opened,
			full:          lengthOfQueue > 0,
		}
		s.rooms.Unlock()
		s.rooms.roomLocks[room].Unlock()
	}

	return
}

func (s *server) deleteRoom(room string) {
	s.rooms.Lock()
	defer s.rooms.Unlock()
	if _, ok := s.rooms.rooms[room]; !ok {
		return
	}
	if s.rooms.rooms[room].queue != nil && s.rooms.rooms[room].queue.Len() > 0 && s.rooms.roomLocks[room] != nil {
		// signal to all waiting that the room will be deleted
		for {
			s.rooms.roomLocks[room].Unlock()
			if s.rooms.rooms[room].queue.Len() == 0 {
				break
			}
			s.rooms.roomLocks[room].Lock()
		}
		delete(s.rooms.roomLocks, room)
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
func ConnectToTCPServer(address, password, room string, isSender, isMainRoom bool, maxOccupants int, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, err error) {
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

	log.Debug("tell server if you want to send or receive")
	clientType := "receive"
	if isSender {
		clientType = "send"
	}
	bSend, err = crypt.Encrypt([]byte(clientType), strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send(bSend)
	if err != nil {
		log.Debug(err)
		return
	}

	log.Debug("sending room")
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

	if isSender {
		log.Debug("tell server if this is a main room")
		roomType := "secondary"
		if isMainRoom {
			roomType = "main"
		}
		bSend, err = crypt.Encrypt([]byte(roomType), strongKeyForEncryption)
		if err != nil {
			log.Debug(err)
			return
		}
		err = c.Send(bSend)
		if err != nil {
			log.Debug(err)
			return
		}

		if isMainRoom {
			log.Debug("tell server maxOccupants")
			bSend, err = crypt.Encrypt([]byte(strconv.Itoa(maxOccupants)), strongKeyForEncryption)
			if err != nil {
				log.Debug(err)
				return
			}
			err = c.Send(bSend)
			if err != nil {
				log.Debug(err)
				return
			}
		}
	}

	log.Debug("waiting for room confirmation")
	for {
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
		if !isSender {
			if bytes.Equal(data, []byte("wait")) {
				log.Debug("waiting for sender to be free")
				time.Sleep(1 * time.Second)
				continue
			} else if bytes.Equal(data, []byte("sender_gone")) {
				c = nil
				return
			} else if bytes.Equal(data, []byte("room full")) {
				c = nil
				return
			}
		}
		if !bytes.Equal(data, []byte("ok")) {
			err = fmt.Errorf("got bad response: %s", data)
			log.Debug(err)
			return
		} else {
			break
		}
	}

	log.Debug("all set")
	return
}
