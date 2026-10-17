package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/pake/v3"
)

func pendingHandshakeCounts(s *server) (int, map[string]int) {
	l := s.pendingHandshakes
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pending, maps.Clone(l.sources)
}

func TestPendingHandshakeLimiterBoundsAndCleanup(t *testing.T) {
	l := newPendingHandshakeLimiter(3, 2)
	var releases []func()
	for _, source := range []string{"a", "a", "b"} {
		if source == "b" {
			if release, why := l.acquire("a"); release != nil || !strings.Contains(why, "source") {
				t.Fatalf("source limit: release=%v, reason=%q", release != nil, why)
			}
		}
		release, why := l.acquire(source)
		if release == nil || why != "" {
			t.Fatalf("allowed reservation rejected: %q", why)
		}
		releases = append(releases, release)
	}
	for i := range 1000 {
		if release, why := l.acquire(fmt.Sprint(i)); release != nil || !strings.Contains(why, "global") {
			t.Fatalf("global limit: release=%v, reason=%q", release != nil, why)
		}
	}
	if l.pending != 3 || len(l.sources) != 2 {
		t.Fatalf("rejections changed accounting: %d, %v", l.pending, l.sources)
	}
	for _, release := range releases {
		release()
		release()
	}
	if l.pending != 0 || len(l.sources) != 0 {
		t.Fatalf("reservations were not released exactly once: %d, %v", l.pending, l.sources)
	}
}

func TestPendingHandshakeLimiterConcurrentReservations(t *testing.T) {
	l := newPendingHandshakeLimiter(64, 32)
	results := make(chan func(), 256)
	for i := range 256 {
		go func() {
			release, _ := l.acquire(fmt.Sprint(i % 8))
			results <- release
		}()
	}
	var releases []func()
	for range 256 {
		if release := <-results; release != nil {
			releases = append(releases, release)
		}
	}
	l.mu.Lock()
	pending, sources := l.pending, maps.Clone(l.sources)
	l.mu.Unlock()
	if pending != 64 || len(releases) != 64 {
		t.Fatalf("concurrent reservations = %d/%d, want 64", pending, len(releases))
	}
	for source, count := range sources {
		if count > 32 {
			t.Fatalf("source %s has %d reservations", source, count)
		}
	}
	var workers sync.WaitGroup
	for _, release := range releases {
		workers.Go(func() { release(); release() })
	}
	workers.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending != 0 || len(l.sources) != 0 {
		t.Fatalf("concurrent release leaked reservations: %d, %v", l.pending, l.sources)
	}
}

func TestPendingHandshakeLimiterNormalizesMappedSources(t *testing.T) {
	l := newPendingHandshakeLimiter(2, 1)
	first := canonicalSource(&net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 1})
	mapped := canonicalSource(&net.TCPAddr{IP: net.ParseIP("::ffff:192.0.2.10"), Port: 2})
	release, _ := l.acquire(first)
	defer release()
	if next, why := l.acquire(mapped); next != nil || !strings.Contains(why, "source") {
		t.Fatalf("mapped source bypassed limit: %q", why)
	}
}

// Give real loopback sockets controlled peer addresses without configuring OS
// IP aliases. Each accepted connection keeps the source captured at acceptance.
type handshakeSourceListener struct {
	net.Listener
	source atomic.Value
	closed atomic.Int32
}

func (l *handshakeSourceListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &handshakeSourceConn{Conn: conn, source: &net.TCPAddr{
		IP: net.ParseIP(l.source.Load().(string)), Port: conn.RemoteAddr().(*net.TCPAddr).Port,
	}, closed: &l.closed}, nil
}

type handshakeSourceConn struct {
	net.Conn
	source net.Addr
	closed *atomic.Int32
	once   sync.Once
}

func (c *handshakeSourceConn) RemoteAddr() net.Addr { return c.source }
func (c *handshakeSourceConn) Close() (err error) {
	c.once.Do(func() { err = c.Conn.Close(); c.closed.Add(1) })
	return
}

func startSourceHandshakeServer(t *testing.T, opts ...serverOptsFunc) (*server, *handshakeSourceListener, string) {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &handshakeSourceListener{Listener: raw}
	listener.source.Store("192.0.2.10")
	ctx, cancel := context.WithCancel(t.Context())
	configured := make(chan *server, 1)
	done := make(chan error, 1)
	options := append([]serverOptsFunc{WithCtx(ctx), WithLogLevel("error"), func(s *server) error {
		configured <- s
		return nil
	}}, opts...)
	go func() { done <- RunWithListener(listener, "pass123", options...) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop source test relay: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("source test relay did not stop")
		}
	})
	s := <-configured
	select {
	case <-s.started:
	case <-time.After(2 * time.Second):
		t.Fatal("source test relay did not start")
	}
	address := raw.Addr().String()
	waitForRelayPort(t, address)
	// Drain our readiness ping before opening held sockets.
	deadline := time.Now().Add(2 * time.Second)
	for listener.closed.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("startup probes did not close")
		}
		time.Sleep(time.Millisecond)
	}
	waitForPendingHandshakes(t, s, 0)
	return s, listener, address
}

func TestPendingHandshakeSourceIsolationAndFastAdmission(t *testing.T) {
	set, err := NewRelayCapabilitySet(1)
	if err != nil {
		t.Fatal(err)
	}
	s, listener, address := startSourceHandshakeServer(t, WithFastAdmission(set))
	_, dataPort, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	controlServer, controlListener, controlAddress := startSourceHandshakeServer(t, WithBanner(dataPort), WithFastAdmission(set))
	controlListener.source.Store("192.0.2.20")
	control, _, _, capability, err := ConnectToTCPServerControl(controlAddress, "pass123", "healthy-parent")
	if err != nil || capability == "" {
		t.Fatalf("issue capability before saturation: %q, %v", capability, err)
	}
	defer control.Close()
	waitForPendingHandshakes(t, controlServer, 0)
	var held []net.Conn
	defer func() {
		for _, conn := range held {
			conn.Close()
		}
	}()
	for i := range 64 {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if i < 32 {
			held = append(held, conn)
			waitForPendingHandshakes(t, s, i+1)
		} else {
			waitForRawConnectionClose(t, conn)
			conn.Close()
		}
	}
	listener.source.Store("192.0.2.20")
	normal, _, _, err := ConnectToTCPServer(address, "pass123", "healthy-normal")
	if err != nil {
		t.Fatalf("another source could not complete PAKE under saturation: %v", err)
	}
	defer normal.Close()
	fast, _, _, usedFast, err := ConnectToTCPServerWithCapability(address, "pass123", "healthy-parent-0", capability)
	if err != nil {
		t.Fatalf("another source could not use its existing capability: %v", err)
	}
	defer fast.Close()
	if !usedFast {
		t.Fatal("valid capability fell back to PAKE")
	}
	waitForPendingHandshakes(t, s, 32)
	pending, sources := pendingHandshakeCounts(s)
	if pending != 32 || len(sources) != 1 || sources["192.0.2.10"] != 32 {
		t.Fatalf("source isolation accounting: %d, %v", pending, sources)
	}
	for _, conn := range held {
		conn.Close()
	}
	waitForPendingHandshakes(t, s, 0)
	_, sources = pendingHandshakeCounts(s)
	if len(sources) != 0 {
		t.Fatalf("established connections retained pending entries: %v", sources)
	}
}

func TestPendingHandshakeFailuresReleaseCapacity(t *testing.T) {
	for _, mode := range []string{"disconnect", "malformed frame", "bad PAKE", "bad password", "bad token", "ping"} {
		t.Run(mode, func(t *testing.T) {
			s, address, stop := startConfiguredTestServer(t, WithMaxPendingHandshakes(1),
				WithMaxPendingHandshakesPerSource(1), WithHandshakeTimeout(2*time.Second), WithFastAdmission(readyCapabilitySet(t, 1)))
			defer stop()
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			waitForPendingHandshakes(t, s, 1)
			c := comm.New(conn)
			switch mode {
			case "disconnect":
				conn.Close()
			case "malformed frame":
				_, err = conn.Write([]byte("invalid!"))
			case "bad PAKE":
				err = c.Send([]byte("not-json"))
			case "bad password":
				if _, _, handshakeErr := HandshakeTCPServer(c, "wrong-password", "room"); handshakeErr == nil {
					t.Fatal("bad password was accepted")
				}
			case "bad token":
				payload, encodeErr := encodeFastAdmissionRequest("invalid-token", "room-0")
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				err = c.Send(payload)
			case "ping":
				err = c.Send([]byte("ping"))
				if err == nil {
					var pong []byte
					pong, err = c.Receive()
					if !bytes.Equal(pong, []byte("pong")) {
						t.Fatalf("ping reply = %q", pong)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "disconnect" {
				waitForRawConnectionClose(t, conn)
			}
			waitForPendingHandshakes(t, s, 0)
			_, sources := pendingHandshakeCounts(s)
			if len(sources) != 0 {
				t.Fatalf("failed handshake retained source entry: %v", sources)
			}
			if err := PingServer(address); err != nil {
				t.Fatalf("released capacity was not reusable: %v", err)
			}
		})
	}
}

func TestFirstMessageDeadlineClosesIncompleteFrames(t *testing.T) {
	for _, mode := range []string{"idle", "partial header", "partial body", "slow body"} {
		t.Run(mode, func(t *testing.T) {
			s, address, stop := startConfiguredTestServer(t, WithFirstMessageTimeout(100*time.Millisecond), WithHandshakeTimeout(3*time.Second))
			defer stop()
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			waitForPendingHandshakes(t, s, 1)
			var header [8]byte
			copy(header[:4], comm.MAGIC_BYTES)
			binary.LittleEndian.PutUint32(header[4:], 128)
			switch mode {
			case "partial header":
				_, err = conn.Write(header[:3])
			case "partial body", "slow body":
				_, err = conn.Write(header[:])
			}
			if err != nil {
				t.Fatal(err)
			}
			stopDrip := make(chan struct{})
			dripDone := make(chan struct{})
			if mode == "slow body" {
				go func() {
					defer close(dripDone)
					ticker := time.NewTicker(20 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-stopDrip:
							return
						case <-ticker.C:
							if _, err := conn.Write([]byte{1}); err != nil {
								return
							}
						}
					}
				}()
				defer func() { close(stopDrip); conn.Close(); <-dripDone }()
			}
			waitForRawConnectionClose(t, conn)
			waitForPendingHandshakes(t, s, 0)
		})
	}
}

type delayedFirstReadConn struct {
	net.Conn
	once sync.Once
}

func (c *delayedFirstReadConn) Read(p []byte) (int, error) {
	c.once.Do(func() { time.Sleep(350 * time.Millisecond) })
	return c.Conn.Read(p)
}

func TestFirstMessageCompletionUsesOverallDeadline(t *testing.T) {
	_, address, stop := startConfiguredTestServer(t, WithFirstMessageTimeout(200*time.Millisecond), WithHandshakeTimeout(2*time.Second))
	defer stop()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := comm.New(&delayedFirstReadConn{Conn: conn})
	if _, _, err := HandshakeTCPServer(c, "pass123", "delayed-rest"); err != nil {
		t.Fatalf("handshake could not continue after first-message deadline: %v", err)
	}

	_, stalledAddress, stopStalled := startConfiguredTestServer(t, WithFirstMessageTimeout(200*time.Millisecond), WithHandshakeTimeout(700*time.Millisecond))
	defer stopStalled()
	stalled, err := comm.NewConnection(stalledAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	initiator, err := pake.InitCurve(weakKey, 0, "siec")
	if err != nil {
		t.Fatal(err)
	}
	if err := stalled.Send(initiator.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := stalled.Receive(); err != nil {
		t.Fatal(err)
	}
	raw := stalled.Connection()
	if err := raw.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := raw.Read(b[:]); err == nil {
		t.Fatal("stalled handshake unexpectedly sent more data")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("first-message deadline closed a later handshake stage: %v", err)
	}
	waitForRawConnectionClose(t, raw)
}
