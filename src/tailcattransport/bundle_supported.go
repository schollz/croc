//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package tailcattransport

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tailscale.com/ipn/ipnstate"
)

type sharedOwner struct {
	closeOnce sync.Once
	closeErr  error
	close     func() error
}

type pathTracker struct {
	mu     sync.Mutex
	path   string
	events PathEvent
}

func newPathTracker(events PathEvent) *pathTracker {
	return &pathTracker{events: events}
}

func (p *pathTracker) set(path string) {
	if p == nil || path == "" {
		return
	}
	p.mu.Lock()
	if path == p.path {
		p.mu.Unlock()
		return
	}
	p.path = path
	events := p.events
	p.mu.Unlock()
	if events != nil {
		events("path=" + path)
	}
}

func (p *pathTracker) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.path
}

func (o *sharedOwner) Close() error {
	if o == nil {
		return nil
	}
	o.closeOnce.Do(func() {
		if o.close != nil {
			o.closeErr = o.close()
		}
	})
	return o.closeErr
}

type countingConn struct {
	net.Conn
	sent     *atomic.Int64
	received *atomic.Int64
}

func (c countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.received.Add(int64(n))
	return n, err
}

func (c countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.sent.Add(int64(n))
	return n, err
}

// Bundle owns all streams and the underlying Tailcat client or server.
type Bundle struct {
	connections []net.Conn
	owner       *sharedOwner
	status      func() *ipnstate.Status
	path        *pathTracker
	setup       time.Duration
	sent        atomic.Int64
	received    atomic.Int64
	stopStatus  chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

func newBundle(connections []net.Conn, started time.Time, owner *sharedOwner, status func() *ipnstate.Status, path *pathTracker) *Bundle {
	if path == nil {
		path = newPathTracker(nil)
	}
	b := &Bundle{
		owner:      owner,
		status:     status,
		path:       path,
		setup:      time.Since(started),
		stopStatus: make(chan struct{}),
	}
	b.connections = make([]net.Conn, len(connections))
	for i, conn := range connections {
		b.connections[i] = countingConn{Conn: conn, sent: &b.sent, received: &b.received}
	}
	b.updatePath()
	go b.watchPath()
	return b
}

// Connections returns the ordered stream set.
func (b *Bundle) Connections() []net.Conn {
	if b == nil {
		return nil
	}
	return append([]net.Conn(nil), b.connections...)
}

func (b *Bundle) watchPath() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.updatePath()
		case <-b.stopStatus:
			return
		}
	}
}

func (b *Bundle) updatePath() {
	path := pathFromStatus(b.status)
	if path == "" && b.path != nil {
		path = b.path.get()
	}
	if path == "" {
		path = "connecting"
	}
	b.path.set(path)
}

func pathFromStatus(status func() *ipnstate.Status) string {
	if status == nil {
		return ""
	}
	st := status()
	if st == nil {
		return ""
	}
	for _, peer := range st.Peer {
		if peer == nil {
			continue
		}
		if peer.CurAddr != "" {
			return "direct"
		}
		if peer.Relay != "" {
			return "derp"
		}
	}
	return ""
}

// Stats returns current stream, path, setup, and logical byte counters.
func (b *Bundle) Stats() BundleStats {
	if b == nil {
		return BundleStats{}
	}
	b.updatePath()
	path := b.path.get()
	return BundleStats{
		Path:          path,
		StreamCount:   len(b.connections),
		SetupDuration: b.setup,
		BytesSent:     b.sent.Load(),
		BytesReceived: b.received.Load(),
	}
}

// Close idempotently closes every stream and the underlying Tailcat node.
func (b *Bundle) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		close(b.stopStatus)
		for _, conn := range b.connections {
			if conn != nil {
				if err := conn.Close(); err != nil && !IsExpectedClose(err) && b.closeErr == nil {
					b.closeErr = err
				}
			}
		}
		if err := b.owner.Close(); err != nil && !IsExpectedClose(err) && b.closeErr == nil {
			b.closeErr = err
		}
	})
	return b.closeErr
}

func closeConnections(connections []net.Conn) {
	for _, conn := range connections {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

// IsExpectedClose recognizes normal transport shutdown and terminal EOF.
func IsExpectedClose(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "closed network connection") || strings.Contains(text, "connection was closed")
}
