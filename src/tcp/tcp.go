package tcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/pake/v3"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/redact"
)

type server struct {
	host       string
	port       string
	debugLevel string
	banner     string
	password   string
	roomPaired func()
	rooms      roomMap
	started    chan struct{}

	maxPendingHandshakes int
	handshakeTimeout     time.Duration
	handshakeSlots       chan struct{}
	maxRoomsOpen         int
	roomCleanupInterval  time.Duration
	roomTTL              time.Duration
	sourceJoinLimit      int
	roomJoinLimit        int
	joinLimitWindow      time.Duration
	admissionLimits      *admissionLimiter

	// stopRoomCleanup chan struct{}
	// replaced by stop ctx.go
	*stop
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

type roomAdmission struct {
	created           bool
	full              bool
	otherConnection   *comm.Comm
	evicted           bool
	evictedRoom       string
	evictedConnection *comm.Comm
}

type handshakeResult struct {
	room                   string
	strongKeyForEncryption []byte
}

const pingRoom = "pinglkasjdlfjsaldjf"

// newDefaultServer initializes a new server, with some default configuration options
func newDefaultServer() *server {
	s := new(server)
	s.maxPendingHandshakes = DEFAULT_MAX_PENDING_HANDSHAKES
	s.handshakeTimeout = DEFAULT_HANDSHAKE_TIMEOUT
	s.maxRoomsOpen = DEFAULT_MAX_ROOMS_OPEN
	s.roomCleanupInterval = DEFAULT_ROOM_CLEANUP_INTERVAL
	s.roomTTL = DEFAULT_ROOM_TTL
	s.sourceJoinLimit = DEFAULT_SOURCE_JOIN_LIMIT
	s.roomJoinLimit = DEFAULT_ROOM_JOIN_LIMIT
	s.joinLimitWindow = DEFAULT_JOIN_LIMIT_WINDOW
	s.debugLevel = DEFAULT_LOG_LEVEL
	s.started = make(chan struct{})
	// s.stopRoomCleanup = make(chan struct{}) replaced by stop
	s.stop = newStop(context.Background())
	return s
}

// admitToRoom atomically creates a waiting room, joins an existing waiting
// room, or reports that an existing room is already full. When creating a room
// at capacity, it removes the oldest waiting room before inserting the new one.
func (s *server) admitToRoom(room string, c *comm.Comm) roomAdmission {
	s.rooms.Lock()
	defer s.rooms.Unlock()

	roomData, ok := s.rooms.rooms[room]
	if ok {
		if roomData.full {
			return roomAdmission{full: true}
		}
		roomData.second = c
		roomData.full = true
		s.rooms.rooms[room] = roomData
		return roomAdmission{otherConnection: roomData.first}
	}

	waitingRooms := 0
	oldestRoomFound := false
	oldestRoom := ""
	var oldestRoomData roomInfo
	for candidate, candidateData := range s.rooms.rooms {
		if candidateData.full {
			continue
		}
		waitingRooms++
		if !oldestRoomFound || candidateData.opened.Before(oldestRoomData.opened) {
			oldestRoomFound = true
			oldestRoom = candidate
			oldestRoomData = candidateData
		}
	}

	result := roomAdmission{created: true}
	if waitingRooms >= s.maxRoomsOpen && oldestRoomFound {
		delete(s.rooms.rooms, oldestRoom)
		result.evicted = true
		result.evictedRoom = oldestRoom
		result.evictedConnection = oldestRoomData.first
	}
	s.rooms.rooms[room] = roomInfo{
		first:  c,
		opened: time.Now(),
	}
	return result
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
	log.Debug("starting relay with configured authentication")

	s.rooms.Lock()
	s.rooms.rooms = make(map[string]roomInfo)
	s.rooms.Unlock()
	s.handshakeSlots = make(chan struct{}, s.maxPendingHandshakes)
	s.admissionLimits = newAdmissionLimiter(s.sourceJoinLimit, s.roomJoinLimit, s.joinLimitWindow)

	s.stop.wg.Go(func() {
		s.deleteOldRooms()
	})
	// defer s.stopRoomDeletion()
	defer s.stop.Cancel()
	if s.stop.gui {
		defer s.stop.wg.Wait()
	}

	err = s.run()
	err = Ignore(err)
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
		if ip.To4() != nil {
			network = "tcp4"
		} else {
			network = "tcp6"
		}

	}
	log.Infof("starting TCP server on %s", addr)
	lc := net.ListenConfig{}
	s.stop.server, err = lc.Listen(s.stop.ctx, network, addr)
	if err != nil {
		return fmt.Errorf("error listening on %s: %w", addr, err)
	}
	defer s.stop.server.Close()
	close(s.started)

	go func() {
		dc := &net.Dialer{
			Timeout: 100 * time.Millisecond,
		}
		if conn, err := dc.DialContext(s.stop.ctx, network, addr); err == nil {
			log.Debugf("started TCP server on %s", addr)
			conn.Close()
		} else {
			log.Errorf("started TCP server on %s : %v", addr, err)
			s.stop.Cancel()
		}
	}()

	// spawn a new goroutine whenever a client connects
	for {
		connection, err := s.stop.server.Accept()
		if err != nil {
			return fmt.Errorf("problem accepting connection: %w", err)
		}
		log.Debugf("client %s connected", connection.RemoteAddr().String())
		select {
		case s.handshakeSlots <- struct{}{}:
		case <-s.stop.ctx.Done():
			connection.Close()
			return s.stop.ctx.Err()
		default:
			log.Debugf("rejecting client %s: too many pending handshakes", connection.RemoteAddr().String())
			connection.Close()
			continue
		}
		handshakeDeadline := time.Now().Add(s.handshakeTimeout)
		s.stop.wg.Add(1)
		go func(connection net.Conn, handshakeDeadline time.Time) {
			defer s.stop.wg.Done()
			handshakePending := true
			releaseHandshake := func() {
				if handshakePending {
					<-s.handshakeSlots
					handshakePending = false
				}
			}
			defer releaseHandshake()
			stopCloseOnCancel := context.AfterFunc(s.stop.ctx, func() {
				connection.Close()
			})
			defer stopCloseOnCancel()

			c := comm.New(connection)
			handshake, errCommunication := s.clientHandshake(c, handshakeDeadline)
			releaseHandshake()
			room := handshake.room
			if errCommunication != nil {
				if netErr, ok := errCommunication.(net.Error); ok && netErr.Timeout() {
					log.Debugf("relay-%s: handshake timed out", connection.RemoteAddr().String())
				} else {
					log.Debugf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
				}
				connection.Close()
				return
			}
			if room == pingRoom {
				log.Debugf("got ping")
				connection.Close()
				return
			}
			if err := connection.SetDeadline(time.Time{}); err != nil {
				log.Debugf("relay-%s: failed to clear handshake deadline: %v", connection.RemoteAddr().String(), err)
				connection.Close()
				return
			}
			room, errCommunication = s.clientCommunication(c, handshake)
			if errCommunication != nil {
				log.Debugf("relay-%s: %s", connection.RemoteAddr().String(), errCommunication.Error())
				connection.Close()
				return
			}
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				// check connection
				log.Tracef("checking waiting relay connection for %+v", c)
				deleteIt := false
				s.rooms.Lock()
				roomData, ok := s.rooms.rooms[room]
				if !ok {
					log.Debug("room is gone")
					s.rooms.Unlock()
					return
				}
				if roomData.first != nil && roomData.second != nil {
					log.Debug("rooms ready")
					s.rooms.Unlock()
					break
				}
				if roomData.first != nil {
					errSend := roomData.first.Send([]byte{1})
					if errSend != nil {
						log.Debug(errSend)
						deleteIt = true
					}
				}
				s.rooms.Unlock()
				if deleteIt {
					s.deleteRoom(room)
					break
				}
				select {
				case <-s.stop.ctx.Done():
					log.Tracef("check: %v", s.stop.ctx.Err())
					s.deleteRoom(room)
					return
				case <-ticker.C:
					// time.Sleep(1 * time.Second)
				}
			}
		}(connection, handshakeDeadline)
	}
}

// deleteOldRooms checks for rooms at a regular interval and removes those that
// have exceeded their allocated TTL.
func (s *server) deleteOldRooms() {
	ticker := time.NewTicker(s.roomCleanupInterval)
	defer func() {
		ticker.Stop()
		log.Debug("room cleanup stopped")
	}()
	for next := true; next; {
		roomsToDelete := []string{}
		select {
		case <-ticker.C:
			s.rooms.Lock()
			for room, roomData := range s.rooms.rooms {
				if time.Since(roomData.opened) > s.roomTTL {
					roomsToDelete = append(roomsToDelete, room)
				}
			}
			s.rooms.Unlock()
		case <-s.stop.ctx.Done():
			if s.server != nil {
				log.Debugf("stop TCP server on %s", s.server.Addr())
				s.server.Close()
				time.Sleep(time.Millisecond)
			}
			log.Debug("stop room cleanup fired")
			s.rooms.Lock()
			for room := range s.rooms.rooms {
				roomsToDelete = append(roomsToDelete, room)
			}
			s.rooms.Unlock()
			next = false
		}
		for _, room := range roomsToDelete {
			s.deleteRoom(room)
			log.Debug("waiting room cleaned up")
		}
	}
}

// replaced by stop
// func (s *server) stopRoomDeletion() {
// 	log.Debug("stop room cleanup fired")
// 	s.stopRoomCleanup <- struct{}{}
// }

var weakKey = []byte{1, 2, 3}

// ErrAdmissionLimited is returned when a relay refuses a room join under its
// configured per-source or per-room admission policy.
var ErrAdmissionLimited = errors.New("relay admission rate limited")

func (s *server) clientHandshake(c *comm.Comm, deadline time.Time) (result handshakeResult, err error) {
	send := func(message []byte) error {
		if err := c.Connection().SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("set handshake write deadline: %w", err)
		}
		return c.Send(message)
	}

	// establish secure password with PAKE for communication with relay
	B, err := pake.InitCurve(weakKey, 1, "siec")
	if err != nil {
		return
	}
	Abytes, err := c.ReceiveWithDeadline(deadline)
	if err != nil {
		return
	}
	log.Debug("received relay PAKE initiator payload")
	if bytes.Equal(Abytes, []byte("ping")) {
		result.room = pingRoom
		log.Debug("sending back pong")
		err = send([]byte("pong"))
		return
	}
	err = B.Update(Abytes)
	if err != nil {
		return
	}
	err = send(B.Bytes())
	if err != nil {
		return
	}
	strongKey, err := B.SessionKey()
	if err != nil {
		return
	}
	// receive salt
	salt, err := c.ReceiveWithDeadline(deadline)
	if err != nil {
		return
	}
	strongKeyForEncryption, _, err := crypt.New(strongKey, salt)
	if err != nil {
		return
	}

	log.Debugf("waiting for password")
	passwordBytesEnc, err := c.ReceiveWithDeadline(deadline)
	if err != nil {
		return
	}
	passwordBytes, err := crypt.Decrypt(passwordBytesEnc, strongKeyForEncryption)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(passwordBytes)) != strings.TrimSpace(s.password) {
		passwordErr := fmt.Errorf("bad password")
		enc, encryptErr := crypt.Encrypt([]byte(passwordErr.Error()), strongKeyForEncryption)
		if encryptErr != nil {
			return handshakeResult{}, encryptErr
		}
		if sendErr := send(enc); sendErr != nil {
			return handshakeResult{}, fmt.Errorf("send error: %w", sendErr)
		}
		return handshakeResult{}, passwordErr
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
	err = send(bSend)
	if err != nil {
		return
	}

	// wait for client to tell me which room they want
	log.Debug("waiting for answer")
	enc, err := c.ReceiveWithDeadline(deadline)
	if err != nil {
		return
	}
	roomBytes, err := crypt.Decrypt(enc, strongKeyForEncryption)
	if err != nil {
		return
	}
	result.room = string(roomBytes)
	result.strongKeyForEncryption = strongKeyForEncryption
	return
}

func (s *server) clientCommunication(c *comm.Comm, handshake handshakeResult) (room string, err error) {
	room = handshake.room
	strongKeyForEncryption := handshake.strongKeyForEncryption
	var bSend []byte
	if s.admissionLimits == nil {
		s.admissionLimits = newAdmissionLimiter(s.sourceJoinLimit, s.roomJoinLimit, s.joinLimitWindow)
	}
	if !s.admissionLimits.allow(canonicalSource(c.Connection().RemoteAddr()), room) {
		bSend, err = crypt.Encrypt([]byte("rate limited"), strongKeyForEncryption)
		if err == nil {
			err = c.Send(bSend)
		}
		if err != nil {
			return room, err
		}
		return room, ErrAdmissionLimited
	}

	admission := s.admitToRoom(room, c)
	if admission.evicted {
		log.Debug("evicting oldest waiting room at capacity")
		if admission.evictedConnection != nil {
			admission.evictedConnection.Close()
		}
	}

	// create the room if it is new
	if admission.created {
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
		log.Debug("room has first peer")
		return
	}
	if admission.full {
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
	log.Debug("room has second peer")
	otherConnection := admission.otherConnection

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
	if s.roomPaired != nil {
		s.roomPaired()
	}
	wg.Wait()

	// delete room
	s.deleteRoom(room)
	return
}

func (s *server) deleteRoom(room string) {
	s.rooms.Lock()
	defer s.rooms.Unlock()
	roomData, ok := s.rooms.rooms[room]
	if !ok {
		return
	}
	log.Debug("deleting room")
	if roomData.first != nil {
		roomData.first.Close()
	}
	if roomData.second != nil {
		roomData.second.Close()
	}
	delete(s.rooms.rooms, room)
}

// pipe creates a full-duplex pipe between the two sockets and
// transfers data from one to the other.
func pipe(conn1 net.Conn, conn2 net.Conn) {
	copyDone := make(chan error, 2)
	copyDirection := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		copyDone <- err
	}
	go copyDirection(conn2, conn1)
	go copyDirection(conn1, conn2)
	if err := <-copyDone; err != nil && !errors.Is(err, net.ErrClosed) {
		log.Debugf("relay pipe closed: %v", err)
	}
}

func PingServer(address string) (err error) {
	_, err = MeasureServerLatency(address, 300*time.Millisecond)
	return err
}

// MeasureServerLatency performs a complete croc ping/pong exchange within one
// overall deadline and returns its elapsed duration.
func MeasureServerLatency(address string, timeout time.Duration) (duration time.Duration, err error) {
	return MeasureServerLatencyContext(context.Background(), address, timeout)
}

// MeasureServerLatencyContext performs one relay probe and closes its
// connection promptly when the caller cancels the probe race.
func MeasureServerLatencyContext(ctx context.Context, address string, timeout time.Duration) (duration time.Duration, err error) {
	log.Debugf("pinging %s", address)
	started := time.Now()
	type connectionResult struct {
		connection *comm.Comm
		err        error
	}
	connected := make(chan connectionResult, 1)
	go func() {
		c, connectErr := comm.NewConnection(address, timeout)
		connected <- connectionResult{connection: c, err: connectErr}
	}()

	var c *comm.Comm
	select {
	case <-ctx.Done():
		// The dial may finish at the same instant as cancellation. Drain its
		// result asynchronously so a successfully opened connection is still
		// closed even when cancellation wins this select.
		go func() {
			result := <-connected
			if result.connection != nil {
				result.connection.Close()
			}
		}()
		return 0, ctx.Err()
	case result := <-connected:
		if result.err != nil {
			log.Debug(result.err)
			return 0, result.err
		}
		c = result.connection
	}
	defer c.Close()
	stopCancel := context.AfterFunc(ctx, func() { c.Close() })
	defer stopCancel()
	deadline := started.Add(timeout)
	if err = c.Connection().SetDeadline(deadline); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		return 0, err
	}
	err = c.Send([]byte("ping"))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		log.Debug(err)
		return
	}
	b, err := c.ReceiveWithDeadline(deadline)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		log.Debug(err)
		return
	}
	if bytes.Equal(b, []byte("pong")) {
		return time.Since(started), nil
	}
	return 0, fmt.Errorf("no pong")
}

// ConnectToTCPServer will initiate a new connection
// to the specified address, room with optional time limit
func ConnectToTCPServer(address, password, room string, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, err error) {
	defer func() { err = redact.Error(err, password, room) }()
	if len(timelimit) > 0 {
		c, err = comm.NewConnection(address, timelimit[0])
	} else {
		c, err = comm.NewConnection(address)
	}
	if err != nil {
		log.Debug(err)
		return
	}
	banner, ipaddr, err = HandshakeTCPServer(c, password, room)
	return
}

// HandshakeTCPServer authenticates and joins a room over an already-open TCP
// connection. Keeping raw dialing separate lets callers race address families
// without admitting the same client to a relay room more than once.
func HandshakeTCPServer(c *comm.Comm, password, room string) (banner string, ipaddr string, err error) {
	defer func() { err = redact.Error(err, password, room) }()

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

	log.Debug("sending encrypted relay authentication")
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

	// The room identifier uses the same relay-session key as the password, so
	// there is no need to wait for the authentication banner before sending it.
	// Existing relays read the frames in their original order and simply find
	// this one already buffered after password verification. This pipelines the
	// last admission flight without changing the wire format.
	log.Debug("sending encrypted room identifier")
	bRoom, err := crypt.Encrypt([]byte(room), strongKeyForEncryption)
	if err != nil {
		log.Debug(err)
		return
	}
	err = c.Send(bRoom)
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
		if bytes.Equal(data, []byte("bad password")) {
			err = fmt.Errorf("bad password")
		} else {
			err = fmt.Errorf("invalid relay response")
		}
		log.Debug(err)
		return
	}
	banner = strings.Split(string(data), "|||")[0]
	ipaddr = strings.Split(string(data), "|||")[1]
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
		if bytes.Equal(data, []byte("rate limited")) {
			err = ErrAdmissionLimited
		} else {
			err = fmt.Errorf("relay admission rejected")
		}
		log.Debug(err)
		return
	}
	log.Debug("all set")
	return
}
