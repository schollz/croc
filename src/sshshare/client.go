//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	internalssh "github.com/schollz/croc/v11/internal/sshclient"
	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/tcp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"tailscale.com/types/key"
)

const (
	defaultReconnectWindow = 2 * time.Minute
	sshHandshakeTimeout    = 30 * time.Second
	tailcatDialTimeout     = 30 * time.Second
)

type authorization struct {
	offer      sshOffer
	control    *comm.Comm
	clientAuth []byte
}

func (a *authorization) close() {
	clear(a.clientAuth)
	a.clientAuth = nil
	if a.control != nil {
		a.control.Close()
		a.control = nil
	}
}

type clientSessionConfig struct {
	config     ClientConfig
	input      *inputBroker
	clientAuth []byte
}

type clientDeps struct {
	request     func(context.Context, string, string, codephrase.SSHComponents, string, key.NodePrivate, Transport) (authorization, error)
	attach      func(context.Context, clientSessionConfig, sshOffer, key.NodePrivate) (bool, error)
	attachRelay func(context.Context, clientSessionConfig, sshOffer, net.Conn) (bool, error)
	now         func() time.Time
	wait        func(context.Context, time.Duration) error
}

func (d clientDeps) withDefaults() clientDeps {
	if d.request == nil {
		d.request = requestOffer
	}
	if d.attach == nil {
		d.attach = attachSSH
	}
	if d.attachRelay == nil {
		d.attachRelay = attachRelaySSH
	}
	if d.now == nil {
		d.now = time.Now
	}
	if d.wait == nil {
		d.wait = waitForRetry
	}
	return d
}

type joinClient struct {
	ctx        context.Context
	config     ClientConfig
	components codephrase.SSHComponents
	relay      string
	deps       clientDeps
	clientKey  key.NodePrivate
	input      *inputBroker
}

type joinAttemptResult struct {
	role      Role
	transport Transport
	offered   bool
	connected bool
	err       error
}

// Join authenticates an invitation, connects to its Tailcat SSH endpoint, and
// attaches the local terminal. After a successful attachment, transient
// disconnects re-run PAKE and reattach to the same shared PTY until the
// reconnect window expires.
func Join(ctx context.Context, config ClientConfig) error {
	return joinWithDeps(ctx, config, clientDeps{})
}

func joinWithDeps(ctx context.Context, config ClientConfig, deps clientDeps) error {
	client, err := newJoinClient(ctx, config, deps)
	if err != nil {
		return err
	}
	return client.run()
}

func newJoinClient(ctx context.Context, config ClientConfig, deps clientDeps) (*joinClient, error) {
	if ctx == nil {
		return nil, errors.New("SSH client context is required")
	}
	components, err := codephrase.ParseSSH(config.Code)
	if err != nil {
		return nil, err
	}
	if config.RelayPassword == "" {
		return nil, errors.New("SSH relay password is required")
	}
	if config.Curve == "" {
		config.Curve = "p256"
	}
	if config.Input == nil {
		config.Input = os.Stdin
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.ErrorOutput == nil {
		config.ErrorOutput = os.Stderr
	}
	if config.Terminal == nil {
		if file, ok := config.Input.(*os.File); ok {
			config.Terminal = file
		}
	}
	if config.ReconnectWindow == 0 {
		config.ReconnectWindow = defaultReconnectWindow
	} else if config.ReconnectWindow < 0 {
		return nil, errors.New("SSH reconnect window must not be negative")
	}
	config.TransportMode = TransportMode(strings.ToLower(strings.TrimSpace(string(config.TransportMode))))
	if config.TransportMode == "" {
		config.TransportMode = TransportModeAuto
	}
	switch config.TransportMode {
	case TransportModeAuto, TransportModeTailcat, TransportModeRelay:
	default:
		return nil, fmt.Errorf("unsupported SSH transport mode %q", config.TransportMode)
	}
	relay, err := relayForCode(config.RelayAddress, config.Code)
	if err != nil {
		return nil, err
	}
	return &joinClient{
		ctx:        ctx,
		config:     config,
		components: components,
		relay:      relay,
		deps:       deps.withDefaults(),
		clientKey:  key.NewNode(),
	}, nil
}

func (c *joinClient) run() error {
	attempt := 0
	attached := false
	var disconnectedAt time.Time
	for {
		attempt++
		c.emit(JoinEvent{State: JoinStateConnecting, Attempt: attempt})
		result := c.runPreferredTransport()
		if result.connected {
			attached = true
			disconnectedAt = c.deps.now()
			attempt = 0
		}
		if result.offered {
			c.emit(JoinEvent{
				State: JoinStateDisconnected, Role: result.role, Transport: result.transport,
				Attempt: max(attempt, 1), Err: result.err,
			})
		}
		if result.err == nil {
			return nil
		}
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		if !attached {
			// Authentication and first-connect failures are actionable and are
			// not hidden behind an automatic retry loop.
			return result.err
		}
		if !c.config.Reconnect {
			return result.err
		}
		if disconnectedAt.IsZero() {
			disconnectedAt = c.deps.now()
		}
		if c.deps.now().Sub(disconnectedAt) >= c.config.ReconnectWindow {
			return fmt.Errorf("SSH reconnect window expired: %w", result.err)
		}
		reconnectAttempt := max(attempt, 1)
		c.emit(JoinEvent{State: JoinStateReconnecting, Attempt: reconnectAttempt, Err: result.err})
		delay := min(time.Duration(reconnectAttempt-1)*500*time.Millisecond, 5*time.Second)
		if err := c.deps.wait(c.ctx, delay); err != nil {
			return err
		}
	}
}

func (c *joinClient) runPreferredTransport() joinAttemptResult {
	transport := TransportTailcat
	if c.config.TransportMode == TransportModeRelay {
		transport = TransportRelay
	}
	result := c.runTransport(transport)
	if c.config.TransportMode != TransportModeAuto || result.err == nil || c.ctx.Err() != nil || (result.connected && !c.config.Reconnect) {
		return result
	}

	relayResult := c.runTransport(TransportRelay)
	relayResult.connected = relayResult.connected || result.connected
	if !relayResult.offered && result.offered {
		relayResult.role = result.role
		relayResult.transport = result.transport
		relayResult.offered = true
	}
	if relayResult.err != nil {
		relayResult.err = errors.Join(
			fmt.Errorf("Tailcat path failed: %w", result.err),
			fmt.Errorf("croc relay fallback failed: %w", relayResult.err),
		)
	}
	return relayResult
}

func (c *joinClient) runTransport(transport Transport) joinAttemptResult {
	auth, err := c.deps.request(
		c.ctx, c.relay, c.config.RelayPassword, c.components, c.config.Curve, c.clientKey, transport,
	)
	if err != nil {
		return joinAttemptResult{transport: transport, err: err}
	}
	defer auth.close()
	result := joinAttemptResult{role: auth.offer.Role, transport: auth.offer.Transport, offered: true}
	if auth.offer.Transport != transport {
		result.err = errors.New("host selected an unexpected SSH transport")
		return result
	}
	if c.input == nil {
		c.input = newInputBroker(c.config.Input)
	}
	sessionConfig := clientSessionConfig{config: c.config, input: c.input, clientAuth: auth.clientAuth}
	if transport == TransportRelay {
		if auth.control == nil || auth.control.Connection() == nil {
			result.err = errors.New("host did not provide a croc relay connection")
			return result
		}
		result.connected, result.err = c.deps.attachRelay(c.ctx, sessionConfig, auth.offer, auth.control.Connection())
		return result
	}
	auth.close()
	result.connected, result.err = c.deps.attach(c.ctx, sessionConfig, auth.offer, c.clientKey)
	return result
}

func (c *joinClient) emit(event JoinEvent) {
	if c.config.OnEvent != nil {
		c.config.OnEvent(event)
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func requestOffer(
	ctx context.Context,
	relay, relayPassword string,
	components codephrase.SSHComponents,
	curve string,
	clientKey key.NodePrivate,
	transport Transport,
) (authorization, error) {
	connection, _, _, _, err := tcp.ConnectToTCPServerControlContext(
		ctx, relay, relayPassword, components.RoomName, 10*time.Second,
	)
	if err != nil {
		return authorization{}, err
	}
	stopClose := context.AfterFunc(ctx, connection.Close)
	defer stopClose()
	encryptionKey, deadline, err := guestPAKE(connection, components, curve)
	if err != nil {
		connection.Close()
		return authorization{}, err
	}
	clientAuth := make([]byte, sshClientAuthSize)
	if _, err = rand.Read(clientAuth); err != nil {
		connection.Close()
		return authorization{}, fmt.Errorf("generate SSH client authentication: %w", err)
	}
	if err = sendAuthorizationRequest(connection, encryptionKey, clientKey.Public(), clientAuth, transport, deadline); err != nil {
		clear(clientAuth)
		connection.Close()
		return authorization{}, err
	}
	offer, err := receiveOffer(connection, encryptionKey, deadline)
	if err != nil {
		clear(clientAuth)
		connection.Close()
		return authorization{}, err
	}
	if offer.Transport != transport {
		clear(clientAuth)
		connection.Close()
		return authorization{}, errors.New("host selected an unexpected SSH transport")
	}
	return authorization{offer: offer, control: connection, clientAuth: clientAuth}, nil
}

func attachSSH(ctx context.Context, config clientSessionConfig, offer sshOffer, clientKey key.NodePrivate) (bool, error) {
	expectedHostKey, err := parseSSHHostKey(offer)
	if err != nil {
		return false, err
	}
	transport := &tailcat.Client{
		Server: tailcat.ConnBlob(offer.TailcatAddress),
		Key:    clientKey,
		Logf:   config.config.Logf,
	}
	defer transport.Close()
	dialCtx, cancel := context.WithTimeout(ctx, tailcatDialTimeout)
	defer cancel()
	connection, err := transport.DialTCPPort(dialCtx, offer.Port)
	if err != nil {
		return false, fmt.Errorf("dial shared SSH terminal: %w", err)
	}
	defer connection.Close()
	return runSSHSession(ctx, config, offer, connection, expectedHostKey)
}

func attachRelaySSH(ctx context.Context, config clientSessionConfig, offer sshOffer, connection net.Conn) (bool, error) {
	expectedHostKey, err := parseSSHHostKey(offer)
	if err != nil {
		return false, err
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		return false, fmt.Errorf("clear SSH relay deadline: %w", err)
	}
	return runSSHSession(ctx, config, offer, connection, expectedHostKey)
}

func parseSSHHostKey(offer sshOffer) (gossh.PublicKey, error) {
	expectedHostKey, err := gossh.ParsePublicKey(offer.SSHHostKey)
	if err != nil {
		return nil, errors.New("host sent an invalid SSH host key")
	}
	return expectedHostKey, nil
}

func runSSHSession(ctx context.Context, config clientSessionConfig, offer sshOffer, connection net.Conn, expectedHostKey gossh.PublicKey) (bool, error) {
	size := normalizeWindowSize(config.config.InitialSize)
	if config.config.Terminal != nil && term.IsTerminal(int(config.config.Terminal.Fd())) {
		if width, height, sizeErr := term.GetSize(int(config.config.Terminal.Fd())); sizeErr == nil {
			size = normalizeWindowSize(WindowSize{Width: width, Height: height})
		}
	}
	terminalName := os.Getenv("TERM")
	if terminalName == "" {
		terminalName = "xterm-256color"
	}
	resizes, stopResize := watchWindowChanges(ctx, config.config.Terminal)
	defer stopResize()

	connected, err := internalssh.Run(ctx, connection, internalssh.Config{
		ExpectedHostKey: expectedHostKey,
		ClientAuth:      config.clientAuth,
		TerminalName:    terminalName,
		InitialSize:     internalssh.WindowSize{Width: size.Width, Height: size.Height},
		PrepareInput: func() (io.Reader, func(), error) {
			input := config.input.activate()
			return input, func() { config.input.deactivate(input) }, nil
		},
		Output:           config.config.Output,
		ErrorOutput:      config.config.ErrorOutput,
		Resizes:          resizes,
		HandshakeTimeout: sshHandshakeTimeout,
		BeforeShell: func() (func(), error) {
			if config.config.Terminal == nil || !term.IsTerminal(int(config.config.Terminal.Fd())) {
				return nil, nil
			}
			state, rawErr := term.MakeRaw(int(config.config.Terminal.Fd()))
			if rawErr != nil {
				return nil, fmt.Errorf("make local terminal raw: %w", rawErr)
			}
			return func() { _ = term.Restore(int(config.config.Terminal.Fd()), state) }, nil
		},
		OnConnected: func() {
			if config.config.OnEvent != nil {
				config.config.OnEvent(JoinEvent{State: JoinStateConnected, Role: offer.Role, Transport: offer.Transport})
			}
		},
	})
	if connected && config.input.Detached() {
		return true, nil
	}
	return connected, err
}

func sshHandshakeError(ctx context.Context, contextDeadline time.Time, err error) error {
	return internalssh.HandshakeError(ctx, contextDeadline, err)
}

type detachReader struct {
	reader   io.Reader
	mu       sync.Mutex
	detached bool
	stopped  bool
}

func (r *detachReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	if r.detached || r.stopped {
		r.mu.Unlock()
		return 0, io.EOF
	}
	r.mu.Unlock()
	n, err := r.reader.Read(buffer)
	detachIndex := bytes.IndexByte(buffer[:n], 0x1d) // Ctrl-]
	stopIndex := bytes.IndexByte(buffer[:n], 0x03)   // Ctrl-C
	index := detachIndex
	if index < 0 || (stopIndex >= 0 && stopIndex < index) {
		index = stopIndex
	}
	if index >= 0 {
		r.mu.Lock()
		r.detached = index == detachIndex
		r.stopped = index == stopIndex
		r.mu.Unlock()
		if index == 0 {
			return 0, io.EOF
		}
		return index, nil
	}
	return n, err
}

func (r *detachReader) Detached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detached
}

func (r *detachReader) Stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}
