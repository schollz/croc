package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/pake/v3"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/redact"
)

type server struct {
	host         string
	port         string
	debugLevel   string
	banner       string
	password     string
	roomPaired   func()
	roomProtocol func(RoomProtocol)
	rooms        roomMap
	started      chan struct{}

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
	fastAdmission        *RelayCapabilitySet

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
	fast                   bool
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
	if request, ok := decodeFastAdmissionRequest(Abytes); ok {
		if s.fastAdmission == nil {
			return handshakeResult{}, errors.New("fast admission is unsupported")
		}
		if err := s.fastAdmission.verifyAndUse(canonicalSource(c.Connection().RemoteAddr()), request.Room, request.Token); err != nil {
			return handshakeResult{}, err
		}
		return handshakeResult{room: request.Room, fast: true}, nil
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

	// New clients pipeline their room frame. Give an upgraded control port a
	// very small window to consume it before the banner so the capability can
	// be bound to the parent room. Legacy clients still receive their banner
	// after this bounded wait and then send the room as before.
	var earlyRoom []byte
	if s.fastAdmission != nil && s.banner != "" {
		earlyDeadline := time.Now().Add(5 * time.Millisecond)
		if earlyDeadline.After(deadline) {
			earlyDeadline = deadline
		}
		earlyRoom, err = c.ReceiveWithDeadline(earlyDeadline)
		if err != nil {
			if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
				return handshakeResult{}, err
			}
			err = nil
		}
	}
	if len(earlyRoom) > 0 {
		roomBytes, decryptErr := crypt.Decrypt(earlyRoom, strongKeyForEncryption)
		if decryptErr != nil {
			return handshakeResult{}, decryptErr
		}
		result.room = string(roomBytes)
	}

	// send ok to tell client they are connected
	banner := s.banner
	if len(banner) == 0 {
		banner = "ok"
	}
	log.Debugf("sending '%s'", banner)
	response := banner + "|||" + c.Connection().RemoteAddr().String()
	if result.room != "" && s.fastAdmission != nil && s.banner != "" {
		if capability, issueErr := s.fastAdmission.issue(canonicalSource(c.Connection().RemoteAddr()), result.room); issueErr == nil {
			response += "|||" + capability
		}
	}
	bSend, err := crypt.Encrypt([]byte(response), strongKeyForEncryption)
	if err != nil {
		return
	}
	err = send(bSend)
	if err != nil {
		return
	}

	// wait for client to tell me which room they want
	log.Debug("waiting for answer")
	if result.room == "" {
		enc, receiveErr := c.ReceiveWithDeadline(deadline)
		if receiveErr != nil {
			return handshakeResult{}, receiveErr
		}
		roomBytes, decryptErr := crypt.Decrypt(enc, strongKeyForEncryption)
		if decryptErr != nil {
			return handshakeResult{}, decryptErr
		}
		result.room = string(roomBytes)
	}
	result.strongKeyForEncryption = strongKeyForEncryption
	return
}

func (s *server) clientCommunication(c *comm.Comm, handshake handshakeResult) (room string, err error) {
	room = handshake.room
	strongKeyForEncryption := handshake.strongKeyForEncryption
	sendAdmission := func(payload string) error {
		if handshake.fast {
			return c.Send([]byte(payload))
		}
		encrypted, encryptErr := crypt.Encrypt([]byte(payload), strongKeyForEncryption)
		if encryptErr != nil {
			return encryptErr
		}
		return c.Send(encrypted)
	}
	if s.admissionLimits == nil {
		s.admissionLimits = newAdmissionLimiter(s.sourceJoinLimit, s.roomJoinLimit, s.joinLimitWindow)
	}
	if !s.admissionLimits.allow(canonicalSource(c.Connection().RemoteAddr()), room) {
		err = sendAdmission("rate limited")
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

		err = sendAdmission("ok")
		if err != nil {
			log.Error(err)
			s.deleteRoom(room)
			return
		}
		log.Debug("room has first peer")
		return
	}
	if admission.full {
		err = sendAdmission("room full")
		if err != nil {
			log.Error(err)
			return
		}
		return
	}
	log.Debug("room has second peer")
	otherConnection := admission.otherConnection

	// Confirm admission before exposing the second peer to frames that the
	// waiting peer may already have buffered. Starting the pipe first can race
	// an application frame ahead of this encrypted confirmation, causing the
	// second client to interpret that frame as a relay response.
	err = sendAdmission("ok")
	if err != nil {
		s.deleteRoom(room)
		return
	}
	if s.roomPaired != nil {
		s.roomPaired()
	}

	// Both peers have completed relay admission; staple their connections.
	var wg sync.WaitGroup
	wg.Add(1)

	// start piping
	go func(com1, com2 *comm.Comm, wg *sync.WaitGroup) {
		log.Debug("starting pipes")
		pipeWithProtocol(com1.Connection(), com2.Connection(), s.roomProtocol)
		wg.Done()
		log.Debug("done piping")
	}(otherConnection, c, &wg)
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
	pipeWithProtocol(conn1, conn2, nil)
}

const relayProtocolMessageLimit = 64 * 1024

type firstFrameObserver struct {
	buffer   []byte
	expected int
	done     bool
	onFrame  func([]byte)
}

func (o *firstFrameObserver) Write(p []byte) (int, error) {
	written := len(p)
	if o.done || len(p) == 0 {
		return written, nil
	}

	for len(p) > 0 && !o.done {
		if o.expected == 0 {
			needed := min(8-len(o.buffer), len(p))
			o.buffer = append(o.buffer, p[:needed]...)
			p = p[needed:]
			if len(o.buffer) < 8 {
				continue
			}
			if !bytes.Equal(o.buffer[:4], comm.MAGIC_BYTES) {
				o.done = true
				break
			}
			bodySize := binary.LittleEndian.Uint32(o.buffer[4:8])
			if bodySize > relayProtocolMessageLimit {
				o.done = true
				break
			}
			o.expected = 8 + int(bodySize)
		}

		needed := min(o.expected-len(o.buffer), len(p))
		o.buffer = append(o.buffer, p[:needed]...)
		p = p[needed:]
		if len(o.buffer) == o.expected {
			o.done = true
			if o.onFrame != nil {
				o.onFrame(o.buffer[8:])
			}
		}
	}
	return written, nil
}

func observedRoomProtocol(payload []byte) (RoomProtocol, bool) {
	m, err := message.DecodeWithLimit(nil, payload, relayProtocolMessageLimit)
	if err != nil || m.Type != message.TypePAKE ||
		!slices.Contains(m.Features, message.FeatureSSHRendezvous) {
		return "", false
	}
	return RoomProtocolSSH, true
}

func copyWithProtocolObservation(dst, src net.Conn, onFrame func([]byte)) error {
	observer := &firstFrameObserver{onFrame: onFrame}
	buffer := make([]byte, 32*1024)
	for !observer.done {
		n, readErr := src.Read(buffer)
		if n > 0 {
			_, _ = observer.Write(buffer[:n])
			remaining := buffer[:n]
			for len(remaining) > 0 {
				written, writeErr := dst.Write(remaining)
				if writeErr != nil {
					return writeErr
				}
				if written == 0 {
					return io.ErrShortWrite
				}
				remaining = remaining[written:]
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
	// Resume the normal net.Conn copy path after the first frame so long-lived
	// transfers can still use the platform's optimized socket forwarding.
	_, err := io.Copy(dst, src)
	return err
}

func pipeWithProtocol(conn1 net.Conn, conn2 net.Conn, callback func(RoomProtocol)) {
	copyDone := make(chan error, 2)
	var reportOnce sync.Once
	copyDirection := func(dst, src net.Conn) {
		var err error
		if callback == nil {
			_, err = io.Copy(dst, src)
		} else {
			err = copyWithProtocolObservation(dst, src, func(payload []byte) {
				protocol, ok := observedRoomProtocol(payload)
				if ok {
					reportOnce.Do(func() { callback(protocol) })
				}
			})
		}
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
	c, err := comm.NewConnectionContext(ctx, address, timeout)
	if err != nil {
		log.Debug(err)
		return 0, err
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
	c, banner, ipaddr, _, err = ConnectToTCPServerControl(address, password, room, timelimit...)
	return
}

// ConnectToTCPServerControl joins a control room and returns an optional fast
// data-admission capability advertised by an upgraded relay.
func ConnectToTCPServerControl(address, password, room string, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, capability string, err error) {
	return ConnectToTCPServerControlContext(context.Background(), address, password, room, timelimit...)
}

// ConnectToTCPServerControlContext joins a control room and closes the
// connection if the caller cancels during dialing or relay authentication.
func ConnectToTCPServerControlContext(ctx context.Context, address, password, room string, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, capability string, err error) {
	defer func() { err = redact.Error(err, password, room) }()
	c, err = comm.NewConnectionContext(ctx, address, timelimit...)
	if err != nil {
		log.Debug(err)
		return
	}
	stopClose := context.AfterFunc(ctx, c.Close)
	defer stopClose()
	banner, ipaddr, capability, err = HandshakeTCPServerCapability(c, password, room)
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	if err != nil {
		c.Close()
		c = nil
		log.Debug(err)
	}
	return
}

// ConnectToTCPServerWithCapability uses the optional upgraded-relay fast path
// and transparently reconnects with the legacy PAKE handshake on rejection.
func ConnectToTCPServerWithCapability(address, password, room, capability string, timelimit ...time.Duration) (c *comm.Comm, banner string, ipaddr string, fast bool, err error) {
	defer func() { err = redact.Error(err, password, room, capability) }()
	if capability != "" {
		timeout := 30 * time.Second
		if len(timelimit) > 0 {
			timeout = timelimit[0]
		}
		c, err = comm.NewConnection(address, timeout)
		if err == nil {
			var request []byte
			request, err = encodeFastAdmissionRequest(capability, room)
			if err == nil {
				err = c.Send(request)
			}
			if err == nil {
				var confirmation []byte
				confirmation, err = c.Receive()
				if err == nil && bytes.Equal(confirmation, []byte("ok")) {
					return c, "", "", true, nil
				}
				if err == nil && bytes.Equal(confirmation, []byte("rate limited")) {
					c.Close()
					return nil, "", "", false, ErrAdmissionLimited
				}
				if err == nil && bytes.Equal(confirmation, []byte("room full")) {
					c.Close()
					return nil, "", "", false, errors.New("relay room full")
				}
			}
			c.Close()
		}
		log.Debug("fast relay admission unavailable; retrying legacy handshake")
	}
	c, banner, ipaddr, err = ConnectToTCPServer(address, password, room, timelimit...)
	return c, banner, ipaddr, false, err
}

// HandshakeTCPServer authenticates and joins a room over an already-open TCP
// connection. Keeping raw dialing separate lets callers race address families
// without admitting the same client to a relay room more than once.
func HandshakeTCPServer(c *comm.Comm, password, room string) (banner string, ipaddr string, err error) {
	banner, ipaddr, _, err = HandshakeTCPServerCapability(c, password, room)
	return
}

// HandshakeTCPServerCapability is HandshakeTCPServer plus the optional opaque
// data-port capability advertised by an upgraded relay.
func HandshakeTCPServerCapability(c *comm.Comm, password, room string) (banner string, ipaddr string, capability string, err error) {
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
	parts := strings.Split(string(data), "|||")
	banner = parts[0]
	ipaddr = parts[1]
	if len(parts) > 2 {
		capability = parts[2]
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
