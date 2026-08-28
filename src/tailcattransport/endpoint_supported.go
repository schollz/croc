//go:build !dragonfly && !netbsd

package tailcattransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

// Listener is a one-shot Tailcat listener for all configured stream ports.
type Listener struct {
	config    Config
	offer     string
	server    *tailcat.Server
	owner     *sharedOwner
	events    PathEvent
	path      *pathTracker
	started   time.Time
	acceptMu  sync.Mutex
	accepted  []bool
	channels  []chan net.Conn
	acceptOne sync.Once
}

// Listen starts a PAKE-bound Tailcat server and returns its self-contained
// encrypted-control-channel offer.
func Listen(ctx context.Context, sessionKey []byte, config Config, events PathEvent) (*Listener, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	identities, err := deriveSessionIdentities(sessionKey)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		config:   config,
		events:   events,
		started:  time.Now(),
		accepted: make([]bool, config.StreamCount),
		channels: make([]chan net.Conn, config.StreamCount),
		path:     newPathTracker(events),
	}
	for i := range l.channels {
		l.channels[i] = make(chan net.Conn, 1)
	}
	ports := make([]filter.PortRange, config.StreamCount)
	for i := range ports {
		port := uint16(FirstTCPPort + i)
		ports[i] = filter.PortRange{First: port, Last: port}
	}
	server := &tailcat.Server{
		Key:            identities.sender,
		AllowedClients: []key.NodePublic{identities.receiver.Public()},
		ServedTCPPorts: ports,
		Logf:           eventLogger(l.path),
	}
	server.OnTCP = l.handlerForPort
	l.server = server
	l.owner = &sharedOwner{close: server.Close}
	if events != nil {
		events("starting Tailcat server")
	}
	if err := server.StartContext(ctx); err != nil {
		_ = l.owner.Close()
		return nil, fmt.Errorf("start Tailcat server: %w", err)
	}
	l.offer = string(server.ConnBlob())
	if err := validateOfferForSender(l.offer, identities.sender.Public()); err != nil {
		_ = l.owner.Close()
		return nil, err
	}
	return l, nil
}

func eventLogger(path *pathTracker) func(string, ...any) {
	return func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		transition := ""
		switch {
		case strings.Contains(line, "via=direct"):
			transition = "direct"
		case strings.Contains(line, "via=derp"):
			transition = "derp"
		}
		path.set(transition)
	}
}

func (l *Listener) handlerForPort(port uint16) func(net.Conn) {
	index := int(port) - FirstTCPPort
	if index < 0 || index >= l.config.StreamCount {
		if l.events != nil {
			l.events(fmt.Sprintf("rejected unexpected Tailcat TCP port %d", port))
		}
		return nil
	}
	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()
	if l.accepted[index] {
		if l.events != nil {
			l.events(fmt.Sprintf("rejected duplicate Tailcat TCP port %d", port))
		}
		return nil
	}
	l.accepted[index] = true
	return func(conn net.Conn) {
		select {
		case l.channels[index] <- conn:
		default:
			_ = conn.Close()
		}
	}
}

// Offer returns the self-contained Tailcat connection blob.
func (l *Listener) Offer() string {
	if l == nil {
		return ""
	}
	return l.offer
}

// Accept waits for every configured stream and transfers server ownership to
// the returned bundle. It may be called only once.
func (l *Listener) Accept(ctx context.Context) (*Bundle, error) {
	if l == nil {
		return nil, errors.New("Tailcat listener is nil")
	}
	called := false
	l.acceptOne.Do(func() { called = true })
	if !called {
		return nil, errors.New("Tailcat listener Accept called more than once")
	}
	connections := make([]net.Conn, l.config.StreamCount)
	for i, ch := range l.channels {
		select {
		case connections[i] = <-ch:
		case <-ctx.Done():
			closeConnections(connections)
			_ = l.owner.Close()
			return nil, ctx.Err()
		}
	}
	return newBundle(connections, l.started, l.owner, l.server.Status, l.path), nil
}

// Close shuts down a listener or a bundle that owns its server.
func (l *Listener) Close() error {
	if l == nil {
		return nil
	}
	return l.owner.Close()
}

// Dial validates offer and concurrently establishes every virtual TCP stream.
func Dial(ctx context.Context, offer string, sessionKey []byte, config Config, events PathEvent) (*Bundle, error) {
	started := time.Now()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	identities, err := deriveSessionIdentities(sessionKey)
	if err != nil {
		return nil, err
	}
	if err := validateOfferForSender(offer, identities.sender.Public()); err != nil {
		return nil, err
	}
	client := &tailcat.Client{
		Server: tailcat.ConnBlob(offer),
		Key:    identities.receiver,
	}
	path := newPathTracker(events)
	client.Logf = eventLogger(path)
	owner := &sharedOwner{close: client.Close}
	connections := make([]net.Conn, config.StreamCount)
	errs := make(chan error, config.StreamCount)
	var wg sync.WaitGroup
	for i := range connections {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			conn, dialErr := client.DialTCPPort(ctx, uint16(FirstTCPPort+index))
			if dialErr == nil {
				connections[index] = conn
			}
			errs <- dialErr
		}(i)
	}
	wg.Wait()
	close(errs)
	for dialErr := range errs {
		if dialErr != nil {
			closeConnections(connections)
			_ = owner.Close()
			return nil, fmt.Errorf("dial Tailcat streams: %w", dialErr)
		}
	}
	return newBundle(connections, started, owner, client.Status, path), nil
}
