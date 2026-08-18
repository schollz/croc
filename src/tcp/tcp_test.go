package tcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func TestMaxRoomsOpenOption(t *testing.T) {
	s := newDefaultServer()
	defer s.stop.Cancel()

	assert.Equal(t, DEFAULT_MAX_ROOMS_OPEN, s.maxRoomsOpen)
	assert.NoError(t, WithMaxRoomsOpen(7)(s))
	assert.Equal(t, 7, s.maxRoomsOpen)
	assert.Error(t, WithMaxRoomsOpen(0)(s))
	assert.Error(t, WithMaxRoomsOpen(-1)(s))
	assert.Equal(t, 7, s.maxRoomsOpen)
}

func BenchmarkRelayForwarding(b *testing.B) {
	payload := bytes.Repeat([]byte("r"), 16*1024*1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.Run("legacy-copy-every-read", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			reader := bytes.NewReader(payload)
			buf := make([]byte, 64*1024)
			for {
				n, err := reader.Read(buf)
				if n > 0 {
					forwarded := make([]byte, n)
					copy(forwarded, buf[:n])
					if _, writeErr := io.Discard.Write(forwarded); writeErr != nil {
						b.Fatal(writeErr)
					}
				}
				if err == io.EOF {
					break
				}
			}
		}
	})
	b.Run("io-copy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			reader := io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
			if _, err := io.Copy(io.Discard, reader); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestHandshakeOptions(t *testing.T) {
	s := newDefaultServer()
	defer s.stop.Cancel()

	assert.Equal(t, DEFAULT_MAX_PENDING_HANDSHAKES, s.maxPendingHandshakes)
	assert.Equal(t, DEFAULT_HANDSHAKE_TIMEOUT, s.handshakeTimeout)

	assert.NoError(t, WithMaxPendingHandshakes(7)(s))
	assert.Equal(t, 7, s.maxPendingHandshakes)
	assert.Error(t, WithMaxPendingHandshakes(0)(s))
	assert.Error(t, WithMaxPendingHandshakes(-1)(s))
	assert.Equal(t, 7, s.maxPendingHandshakes)

	assert.NoError(t, WithHandshakeTimeout(2*time.Minute)(s))
	assert.Equal(t, 2*time.Minute, s.handshakeTimeout)
	assert.Error(t, WithHandshakeTimeout(0)(s))
	assert.Error(t, WithHandshakeTimeout(-time.Second)(s))
	assert.Equal(t, 2*time.Minute, s.handshakeTimeout)
}

func TestAdmitToRoomEvictsOldestWaitingRoom(t *testing.T) {
	s := newDefaultServer()
	defer s.stop.Cancel()
	s.maxRoomsOpen = 2
	s.rooms.rooms = map[string]roomInfo{
		"oldest": {
			opened: time.Now().Add(-2 * time.Minute),
		},
		"newer": {
			opened: time.Now().Add(-time.Minute),
		},
	}

	result := s.admitToRoom("incoming", nil)

	assert.True(t, result.created)
	assert.True(t, result.evicted)
	assert.Equal(t, "oldest", result.evictedRoom)
	assert.NotContains(t, s.rooms.rooms, "oldest")
	assert.Contains(t, s.rooms.rooms, "newer")
	assert.Contains(t, s.rooms.rooms, "incoming")
}

func TestAdmitToRoomJoinsAtCapacityAndExcludesFullRooms(t *testing.T) {
	s := newDefaultServer()
	defer s.stop.Cancel()
	s.maxRoomsOpen = 1
	s.rooms.rooms = map[string]roomInfo{
		"joining": {
			opened: time.Now().Add(-time.Minute),
		},
	}

	joined := s.admitToRoom("joining", nil)
	assert.False(t, joined.created)
	assert.False(t, joined.full)
	assert.False(t, joined.evicted)
	assert.True(t, s.rooms.rooms["joining"].full)

	created := s.admitToRoom("waiting", nil)
	assert.True(t, created.created)
	assert.False(t, created.evicted)
	assert.Contains(t, s.rooms.rooms, "joining")
	assert.Contains(t, s.rooms.rooms, "waiting")

	evicted := s.admitToRoom("replacement", nil)
	assert.True(t, evicted.evicted)
	assert.Equal(t, "waiting", evicted.evictedRoom)
	assert.Contains(t, s.rooms.rooms, "joining")
	assert.NotContains(t, s.rooms.rooms, "waiting")
	assert.Contains(t, s.rooms.rooms, "replacement")
}

func TestConcurrentRoomAdmissionRespectsWaitingRoomLimit(t *testing.T) {
	s := newDefaultServer()
	defer s.stop.Cancel()
	s.maxRoomsOpen = 8
	s.rooms.rooms = make(map[string]roomInfo)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(room int) {
			defer wg.Done()
			<-start
			s.admitToRoom(fmt.Sprintf("room-%d", room), nil)
		}(i)
	}
	close(start)
	wg.Wait()

	s.rooms.Lock()
	defer s.rooms.Unlock()
	waitingRooms := 0
	for _, roomData := range s.rooms.rooms {
		if !roomData.full {
			waitingRooms++
		}
	}
	assert.Equal(t, s.maxRoomsOpen, waitingRooms)
	assert.Len(t, s.rooms.rooms, s.maxRoomsOpen)
}

func TestWaitingRoomCapacityDisconnectsOldestOccupant(t *testing.T) {
	address, stopServer := startTestServer(t, 1)
	defer stopServer()

	oldest, _, _, err := ConnectToTCPServer(address, "pass123", "oldest")
	if err != nil {
		t.Fatalf("connect oldest room: %v", err)
	}
	defer oldest.Close()

	replacement, _, _, err := ConnectToTCPServer(address, "pass123", "replacement")
	if err != nil {
		t.Fatalf("connect replacement room: %v", err)
	}
	defer replacement.Close()

	waitForConnectionClose(t, oldest)

	peer, _, _, err := ConnectToTCPServer(address, "pass123", "replacement")
	if err != nil {
		t.Fatalf("join replacement room: %v", err)
	}
	defer peer.Close()

	want := []byte("replacement room remains usable")
	if err := peer.Send(want); err != nil {
		t.Fatalf("send through replacement room: %v", err)
	}
	for {
		got, receiveErr := replacement.Receive()
		if receiveErr != nil {
			t.Fatalf("receive through replacement room: %v", receiveErr)
		}
		if bytes.Equal(got, []byte{1}) {
			continue
		}
		assert.Equal(t, want, got)
		break
	}
}

func TestPendingHandshakeLimitRejectsExcessConnections(t *testing.T) {
	s, address, stopServer := startConfiguredTestServer(t,
		WithMaxPendingHandshakes(2),
		WithHandshakeTimeout(2*time.Second),
	)
	defer stopServer()

	first := openPartialHandshake(t, address)
	defer first.Close()
	second := openPartialHandshake(t, address)
	defer second.Close()
	waitForPendingHandshakes(t, s, 2)

	excess, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial excess connection: %v", err)
	}
	defer excess.Close()
	waitForRawConnectionClose(t, excess)
	assert.Equal(t, 2, len(s.handshakeSlots))

	first.Close()
	waitForPendingHandshakes(t, s, 1)
	if err := PingServer(address); err != nil {
		t.Fatalf("handshake slot was not reusable: %v", err)
	}
}

func TestPendingHandshakeDeadlineClosesIdleAndPartialConnections(t *testing.T) {
	s, address, stopServer := startConfiguredTestServer(t,
		WithMaxPendingHandshakes(1),
		WithHandshakeTimeout(75*time.Millisecond),
	)
	defer stopServer()

	idle, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial idle connection: %v", err)
	}
	waitForPendingHandshakes(t, s, 1)
	waitForRawConnectionClose(t, idle)
	idle.Close()
	waitForPendingHandshakes(t, s, 0)

	partial := openPartialHandshake(t, address)
	waitForPendingHandshakes(t, s, 1)
	waitForRawConnectionClose(t, partial)
	partial.Close()
	waitForPendingHandshakes(t, s, 0)

	if err := PingServer(address); err != nil {
		t.Fatalf("timed-out handshake slot was not reusable: %v", err)
	}
}

func TestEstablishedTransferOutlivesHandshakeDeadline(t *testing.T) {
	_, address, stopServer := startConfiguredTestServer(t,
		WithHandshakeTimeout(500*time.Millisecond),
	)
	defer stopServer()

	first, _, _, err := ConnectToTCPServer(address, "pass123", "deadline-cleared")
	if err != nil {
		t.Fatalf("connect first client: %v", err)
	}
	defer first.Close()
	second, _, _, err := ConnectToTCPServer(address, "pass123", "deadline-cleared")
	if err != nil {
		t.Fatalf("connect second client: %v", err)
	}
	defer second.Close()

	time.Sleep(600 * time.Millisecond)
	want := []byte("transfer remains connected")
	if err := first.Send(want); err != nil {
		t.Fatalf("send after handshake deadline: %v", err)
	}
	for {
		got, receiveErr := second.Receive()
		if receiveErr != nil {
			t.Fatalf("receive after handshake deadline: %v", receiveErr)
		}
		if bytes.Equal(got, []byte{1}) {
			continue
		}
		assert.Equal(t, want, got)
		break
	}
}

func TestServerCancellationClosesPendingHandshake(t *testing.T) {
	s, address, stopServer := startConfiguredTestServer(t,
		WithHandshakeTimeout(5*time.Minute),
	)

	connection := openPartialHandshake(t, address)
	defer connection.Close()
	waitForPendingHandshakes(t, s, 1)

	started := time.Now()
	stopServer()
	assert.Less(t, time.Since(started), time.Second)
}

func startTestServer(t *testing.T, maxRoomsOpen int) (string, func()) {
	t.Helper()
	_, address, stopServer := startConfiguredTestServer(t, WithMaxRoomsOpen(maxRoomsOpen))
	return address, stopServer
}

func startConfiguredTestServer(t *testing.T, opts ...serverOptsFunc) (*server, string, func()) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	_, port, err := net.SplitHostPort(probe.Addr().String())
	if err != nil {
		probe.Close()
		t.Fatalf("split test address: %v", err)
	}
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s := newDefaultServer()
	s.host = "127.0.0.1"
	s.port = port
	s.password = "pass123"
	baseOpts := []serverOptsFunc{WithCtx(ctx), WithLogLevel("error")}
	for _, opt := range append(baseOpts, opts...) {
		if err := opt(s); err != nil {
			cancel()
			t.Fatalf("configure test server: %v", err)
		}
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- s.start()
	}()

	address := net.JoinHostPort("127.0.0.1", port)
	select {
	case <-s.started:
	case runErr := <-serverErr:
		cancel()
		t.Fatalf("test server stopped during startup: %v", runErr)
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("test server did not start listening")
	}
	deadline := time.Now().Add(2 * time.Second)
	for PingServer(address) != nil {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("test server did not become reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForPendingHandshakes(t, s, 0)

	var once sync.Once
	return s, address, func() {
		once.Do(func() {
			cancel()
			select {
			case runErr := <-serverErr:
				if runErr != nil {
					t.Errorf("stop test server: %v", runErr)
				}
			case <-time.After(2 * time.Second):
				t.Error("test server did not stop")
			}
		})
	}
}

func openPartialHandshake(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial partial handshake: %v", err)
	}
	if _, err := connection.Write([]byte("c")); err != nil {
		connection.Close()
		t.Fatalf("write partial handshake: %v", err)
	}
	return connection
}

func waitForPendingHandshakes(t *testing.T, s *server, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(s.handshakeSlots) != want {
		if time.Now().After(deadline) {
			t.Fatalf("pending handshakes = %d, want %d", len(s.handshakeSlots), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForRawConnectionClose(t *testing.T, connection net.Conn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set close detection deadline: %v", err)
	}
	buffer := make([]byte, 1)
	_, err := connection.Read(buffer)
	if err == nil {
		t.Fatal("connection remained readable")
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("connection remained open")
	}
}

func waitForConnectionClose(t *testing.T, c interface {
	Receive() ([]byte, error)
	Close()
}) {
	t.Helper()
	closed := make(chan error, 1)
	go func() {
		for {
			_, err := c.Receive()
			if err != nil {
				closed <- err
				return
			}
		}
	}()

	select {
	case err := <-closed:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		c.Close()
		t.Fatal("oldest waiting-room occupant remained connected")
	}
}

func BenchmarkConnection(b *testing.B) {
	log.SetLevel("trace")
	go Run("debug", "127.0.0.1", "8283", "pass123", "8284")
	time.Sleep(100 * time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _, _, _ := ConnectToTCPServer("127.0.0.1:8283", "pass123", fmt.Sprintf("testroom%d", i), 1*time.Minute)
		if c != nil {
			c.Close()
		}
	}
}

func TestTCP(t *testing.T) {
	log.SetLevel("error")
	timeToRoomDeletion := 100 * time.Millisecond
	go RunWithOptionsAsync("127.0.0.1", "8381", "pass123",
		WithBanner("8382"),
		WithLogLevel("debug"),
		WithRoomTTL(timeToRoomDeletion))

	time.Sleep(timeToRoomDeletion)
	err := PingServer("127.0.0.1:8381")
	assert.Nil(t, err)
	err = PingServer("127.0.0.1:8333")
	assert.NotNil(t, err)

	time.Sleep(timeToRoomDeletion)
	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Minute)
	assert.Equal(t, banner, "8382")
	assert.Nil(t, err)
	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.Nil(t, err)
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.NotNil(t, err)
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Nanosecond)
	assert.NotNil(t, err)

	// try sending data
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	var data []byte
	for {
		data, err = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c2"), data)

	assert.Nil(t, c2.Send([]byte("hello, c1")))
	for {
		data, err = c1.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c1"), data)

	c1.Close()
	time.Sleep(300 * time.Millisecond)
}

func TestTCPctx(t *testing.T) {
	log.SetLevel("error")
	// Set short room TTL for testing cleanup
	timeToRoomDeletion := 100 * time.Millisecond

	// Create cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server with custom options
	go RunWithOptionsAsync("127.0.0.1", "8381", "pass123",
		WithBanner("8382"),
		WithLogLevel("debug"),
		WithRoomTTL(timeToRoomDeletion),
		WithCtx(ctx),
	)

	time.Sleep(timeToRoomDeletion)

	// Test ping to running server
	err := PingServer("127.0.0.1:8381")
	assert.Nil(t, err)

	// Test ping to non-existent server
	err = PingServer("127.0.0.1:8333")
	assert.NotNil(t, err)

	time.Sleep(timeToRoomDeletion)

	// Connect first client to room
	c1, banner, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Minute)
	assert.Equal(t, banner, "8382")
	assert.Nil(t, err)

	// Connect second client to same room
	c2, _, _, err := ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.Nil(t, err)

	// Third client should fail - room is full
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom")
	assert.NotNil(t, err)

	// Connection with very short timeout should fail
	_, _, _, err = ConnectToTCPServer("127.0.0.1:8381", "pass123", "testRoom", 1*time.Nanosecond)
	assert.NotNil(t, err)

	// Test data exchange between clients
	// Send from c1 to c2
	assert.Nil(t, c1.Send([]byte("hello, c2")))
	var data []byte
	for {
		data, err = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue // Skip heartbeat
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c2"), data)

	// Send from c2 to c1
	assert.Nil(t, c2.Send([]byte("hello, c1")))
	for {
		data, err = c1.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue // Skip heartbeat
		}
		break
	}
	assert.Nil(t, err)
	assert.Equal(t, []byte("hello, c1"), data)

	// Close server
	cancel()

	// Test ping to non-existent server
	err = PingServer("127.0.0.1:8331")
	assert.NotNil(t, err)

	time.Sleep(300 * time.Millisecond)
}

func TestWrongPassword(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8385", "pass123", "8386")
	time.Sleep(100 * time.Millisecond)

	// Attempt to connect with wrong password
	_, _, _, err := ConnectToTCPServer("127.0.0.1:8385", "wrongpass", "testRoom")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "bad password")
}

// A relay password with trailing whitespace should still accept a trimmed client password.
func TestPasswordWithTrailingWhitespace(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8395", "pass123 ", "8396")
	time.Sleep(100 * time.Millisecond)

	_, _, _, err := ConnectToTCPServer("127.0.0.1:8395", "pass123", "testRoom")
	assert.Nil(t, err)
}

func TestRoomIsolation(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8387", "pass123", "8388")
	time.Sleep(100 * time.Millisecond)

	// Room 1
	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room1")
	c2, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room1")

	// Room 2
	c3, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room2")
	c4, _, _, _ := ConnectToTCPServer("127.0.0.1:8387", "pass123", "room2")

	// Send data in different rooms
	c1.Send([]byte("to_room_1"))
	c3.Send([]byte("to_room_2"))

	// Verify reception
	var data []byte

	// c2 should receive message from room1
	for {
		data, _ = c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Equal(t, []byte("to_room_1"), data)

	// c4 should receive message from room2
	for {
		data, _ = c4.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		break
	}
	assert.Equal(t, []byte("to_room_2"), data)

	c1.Close()
	c2.Close()
	c3.Close()
	c4.Close()
}

func TestRoomRecreationAfterTTL(t *testing.T) {
	log.SetLevel("error")
	shortTTL := 50 * time.Millisecond

	go RunWithOptionsAsync("127.0.0.1", "8389", "pass123",
		WithRoomTTL(shortTTL),
		WithLogLevel("error"))
	time.Sleep(100 * time.Millisecond)

	roomName := "testRoomRecreate"

	// 1. Create a room
	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8389", "pass123", roomName)
	assert.NotNil(t, c1)

	// 2. Close first client, room becomes empty
	c1.Close()

	// 3. Wait for room cleanup (TTL + buffer)
	time.Sleep(shortTTL + 50*time.Millisecond)

	// 4. Try to connect to the same room again.
	// If room wasn't deleted, we might get "room full" or weird behavior.
	// If deleted — connection should succeed as the first client.
	c3, _, _, err := ConnectToTCPServer("127.0.0.1:8389", "pass123", roomName)
	assert.Nil(t, err)
	assert.NotNil(t, c3)

	if c3 != nil {
		c3.Close()
	}
}

func TestLargeDataTransfer(t *testing.T) {
	log.SetLevel("error")
	go Run("debug", "127.0.0.1", "8391", "pass123", "8392")
	time.Sleep(100 * time.Millisecond)

	c1, _, _, _ := ConnectToTCPServer("127.0.0.1:8391", "pass123", "bigRoom")
	c2, _, _, _ := ConnectToTCPServer("127.0.0.1:8391", "pass123", "bigRoom")

	// Generate data larger than standard buffer (e.g., 1 MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err := c1.Send(largeData)
	assert.Nil(t, err)

	var received []byte
	// Receive data, as it might arrive in chunks (though chanFromConn buffers it)
	// In this case pipe passes full Read packets, but for safety let's verify tail
	for {
		data, err := c2.Receive()
		if bytes.Equal(data, []byte{1}) {
			continue
		}
		assert.Nil(t, err)
		received = data
		break
	}

	assert.True(t, bytes.Equal(largeData, received), "Large data mismatch")

	c1.Close()
	c2.Close()
}

func TestServerReleasesPort(t *testing.T) {
	log.SetLevel("trace")
	host := "127.0.0.1"
	port := "8394"

	// 1. Start and automatically stop first server using timeout
	// RunCtx blocks the execution, so we don't need 'go' or channels
	ctx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()

	err := RunCtx(ctx1, "trace", host, port, "pass123")
	assert.Nil(t, err, "First server should stop gracefully")

	// 2. Try to start second server on the same port immediately
	// If port is not released, this will fail with "address already in use"
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()

	err = RunCtx(ctx2, "trace", host, port, "pass123")
	assert.Nil(t, err, "Second server should start (port was released)")
}

func TestServerHonorsIPv4LoopbackBind(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.2:0")
	if err != nil {
		t.Skipf("alternate IPv4 loopback address is unavailable: %v", err)
	}
	probe.Close()

	address, stopServer := startTestServer(t, DEFAULT_MAX_ROOMS_OPEN)
	defer stopServer()

	if err := PingServer(address); err != nil {
		t.Fatalf("server is not reachable on its configured loopback address: %v", err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}

	alternateAddress := net.JoinHostPort("127.0.0.2", port)
	connection, err := net.DialTimeout("tcp4", alternateAddress, 100*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatalf("server configured for %s also accepted a connection on %s", address, alternateAddress)
	}
}

func TestMeasureServerLatencyContextCancellationClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		close(accepted)
		buffer := make([]byte, 64)
		for {
			if _, readErr := connection.Read(buffer); readErr != nil {
				close(closed)
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, probeErr := MeasureServerLatencyContext(ctx, listener.Addr().String(), 5*time.Second)
		result <- probeErr
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("probe did not connect")
	}
	cancel()

	select {
	case probeErr := <-result:
		assert.ErrorIs(t, probeErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled probe did not return promptly")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("canceled probe did not close its connection")
	}
}

func TestDualStackRelayBridgesIPv4AndIPv6(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	_, port, err := net.SplitHostPort(probe.Addr().String())
	if err != nil {
		probe.Close()
		t.Fatalf("split probe address: %v", err)
	}
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- RunCtx(ctx, "error", "", port, "pass123")
	}()
	defer func() {
		cancel()
		select {
		case <-serverErr:
		case <-time.After(time.Second):
		}
	}()

	ipv4Address := net.JoinHostPort("127.0.0.1", port)
	ipv6Address := net.JoinHostPort("::1", port)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if PingServer(ipv4Address) == nil && PingServer(ipv6Address) == nil {
			break
		}
		select {
		case err := <-serverErr:
			t.Fatalf("dual-stack relay stopped during startup: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay did not become reachable over both IPv4 and IPv6")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ipv4Client, _, _, err := ConnectToTCPServer(ipv4Address, "pass123", "dual-stack-room")
	if err != nil {
		t.Fatalf("connect IPv4 client: %v", err)
	}
	defer ipv4Client.Close()
	ipv6Client, _, _, err := ConnectToTCPServer(ipv6Address, "pass123", "dual-stack-room")
	if err != nil {
		t.Fatalf("connect IPv6 client: %v", err)
	}
	defer ipv6Client.Close()

	want := []byte("local relay payload")
	if err := ipv4Client.Send(want); err != nil {
		t.Fatalf("send IPv4 payload: %v", err)
	}
	var got []byte
	for {
		got, err = ipv6Client.Receive()
		if err != nil {
			t.Fatalf("receive IPv6 payload: %v", err)
		}
		if !bytes.Equal(got, []byte{1}) {
			break
		}
	}
	assert.Equal(t, want, got)
}
