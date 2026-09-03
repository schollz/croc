//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/tcp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"tailscale.com/types/key"
)

const defaultReconnectWindow = 2 * time.Minute

// TransportMode controls which terminal transport a guest may use.
type TransportMode string

const (
	TransportModeAuto    TransportMode = "auto"
	TransportModeTailcat TransportMode = "tailcat"
	TransportModeRelay   TransportMode = "relay"
)

// JoinEvent describes guest-visible connection state.
type JoinEvent struct {
	State     string
	Role      Role
	Transport Transport
	Attempt   int
	Err       error
}

// ClientConfig configures a participant joining a shared terminal.
type ClientConfig struct {
	Code            string
	RelayAddress    string
	RelayPassword   string
	Curve           string
	Input           io.Reader
	Output          io.Writer
	ErrorOutput     io.Writer
	Terminal        *os.File
	InitialSize     WindowSize
	Reconnect       bool
	ReconnectWindow time.Duration
	TransportMode   TransportMode
	OnEvent         func(JoinEvent)
	Logf            func(string, ...any)
	inputBroker     *inputBroker
	request         func(context.Context, string, string, codephrase.SSHComponents, string, Transport) (Offer, key.NodePrivate, *comm.Comm, error)
	attach          func(context.Context, ClientConfig, Offer, key.NodePrivate) (bool, error)
	attachRelay     func(context.Context, ClientConfig, Offer, net.Conn) (bool, error)
}

// Join authenticates an invitation, connects to its Tailcat SSH endpoint, and
// attaches the local terminal. After a successful attachment, transient
// disconnects re-run PAKE and reattach to the same shared PTY until the
// reconnect window expires.
func Join(ctx context.Context, config ClientConfig) error {
	if ctx == nil {
		return errors.New("SSH client context is required")
	}
	components, err := codephrase.ParseSSH(config.Code)
	if err != nil {
		return err
	}
	if config.RelayPassword == "" {
		return errors.New("SSH relay password is required")
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
	if config.ReconnectWindow <= 0 {
		config.ReconnectWindow = defaultReconnectWindow
	}
	if config.TransportMode == "" {
		config.TransportMode = TransportModeAuto
	}
	switch config.TransportMode {
	case TransportModeAuto, TransportModeTailcat, TransportModeRelay:
	default:
		return fmt.Errorf("unsupported SSH transport mode %q", config.TransportMode)
	}
	relay, err := relayForCode(config.RelayAddress, config.Code)
	if err != nil {
		return err
	}
	request := config.request
	if request == nil {
		request = requestOffer
	}
	attach := config.attach
	if attach == nil {
		attach = attachSSH
	}
	attachRelay := config.attachRelay
	if attachRelay == nil {
		attachRelay = attachRelaySSH
	}

	attempt := 0
	attached := false
	var disconnectedAt time.Time
	for {
		attempt++
		if config.OnEvent != nil {
			config.OnEvent(JoinEvent{State: "connecting", Attempt: attempt})
		}
		requestedTransport := TransportTailcat
		if config.TransportMode == TransportModeRelay {
			requestedTransport = TransportRelay
		}
		offer, clientKey, control, err := request(
			ctx, relay, config.RelayPassword, components, config.Curve, requestedTransport,
		)
		if err == nil {
			if config.inputBroker == nil {
				config.inputBroker = newInputBroker(config.Input)
			}
			var connected bool
			activeOffer := offer
			if requestedTransport == TransportRelay {
				if control == nil {
					err = errors.New("host did not provide a croc relay connection")
				} else {
					connected, err = attachRelay(ctx, config, offer, control.Connection())
				}
			} else {
				if control != nil {
					control.Close()
					control = nil
				}
				connected, err = attach(ctx, config, offer, clientKey)
			}
			if config.TransportMode == TransportModeAuto && err != nil && ctx.Err() == nil && (!connected || config.Reconnect) {
				tailcatErr := err
				var relayControl *comm.Comm
				var relayErr error
				activeOffer, _, relayControl, relayErr = request(
					ctx, relay, config.RelayPassword, components, config.Curve, TransportRelay,
				)
				if relayErr == nil && relayControl == nil {
					relayErr = errors.New("host did not provide a croc relay connection")
				}
				if relayErr == nil {
					var relayConnected bool
					relayConnected, relayErr = attachRelay(ctx, config, activeOffer, relayControl.Connection())
					connected = connected || relayConnected
				}
				if relayControl != nil {
					relayControl.Close()
				}
				if relayErr != nil {
					err = fmt.Errorf("Tailcat path failed: %v; croc relay fallback failed: %w", tailcatErr, relayErr)
				} else {
					err = nil
				}
			}
			if control != nil {
				control.Close()
			}
			attached = attached || connected
			if connected {
				disconnectedAt = time.Now()
				attempt = 0
			}
			if config.OnEvent != nil {
				config.OnEvent(JoinEvent{
					State: "disconnected", Role: activeOffer.Role, Transport: activeOffer.Transport,
					Attempt: attempt, Err: err,
				})
			}
		}
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		if !attached {
			// Authentication and first-connect failures are actionable and are
			// not hidden behind an automatic retry loop.
			return err
		}
		if !config.Reconnect {
			return err
		}
		if disconnectedAt.IsZero() {
			disconnectedAt = time.Now()
		}
		if time.Since(disconnectedAt) >= config.ReconnectWindow {
			return fmt.Errorf("SSH reconnect window expired: %w", err)
		}
		if config.OnEvent != nil {
			config.OnEvent(JoinEvent{State: "reconnecting", Attempt: attempt, Err: err})
		}
		delay := min(time.Duration(attempt)*500*time.Millisecond, 5*time.Second)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		// requestOffer has to use a fresh Tailcat node key each time.
	}
}

func requestOffer(
	ctx context.Context,
	relay, relayPassword string,
	components codephrase.SSHComponents,
	curve string,
	transport Transport,
) (Offer, key.NodePrivate, *comm.Comm, error) {
	clientKey := key.NewNode()
	connection, _, _, _, err := tcp.ConnectToTCPServerControl(
		relay, relayPassword, components.RoomName, 10*time.Second,
	)
	if err != nil {
		return Offer{}, key.NodePrivate{}, nil, err
	}
	stopClose := context.AfterFunc(ctx, connection.Close)
	defer stopClose()
	encryptionKey, err := guestPAKE(connection, components, curve)
	if err != nil {
		connection.Close()
		return Offer{}, key.NodePrivate{}, nil, err
	}
	if err = sendAuthorizationRequest(connection, encryptionKey, clientKey.Public(), transport); err != nil {
		connection.Close()
		return Offer{}, key.NodePrivate{}, nil, err
	}
	offer, err := receiveOffer(connection, encryptionKey)
	if err != nil {
		connection.Close()
		return Offer{}, key.NodePrivate{}, nil, err
	}
	if offer.Transport != transport {
		connection.Close()
		return Offer{}, key.NodePrivate{}, nil, errors.New("host selected an unexpected SSH transport")
	}
	return offer, clientKey, connection, nil
}

func attachSSH(ctx context.Context, config ClientConfig, offer Offer, clientKey key.NodePrivate) (bool, error) {
	expectedHostKey, err := parseSSHHostKey(offer)
	if err != nil {
		return false, err
	}
	transport := &tailcat.Client{
		Server: tailcat.ConnBlob(offer.TailcatAddress),
		Key:    clientKey,
		Logf:   config.Logf,
	}
	defer transport.Close()
	connection, err := transport.DialTCPPort(ctx, offer.Port)
	if err != nil {
		return false, fmt.Errorf("dial shared SSH terminal: %w", err)
	}
	defer connection.Close()
	return runSSHSession(ctx, config, offer, connection, expectedHostKey)
}

func attachRelaySSH(ctx context.Context, config ClientConfig, offer Offer, connection net.Conn) (bool, error) {
	expectedHostKey, err := parseSSHHostKey(offer)
	if err != nil {
		return false, err
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		return false, fmt.Errorf("clear SSH relay deadline: %w", err)
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	return runSSHSession(ctx, config, offer, connection, expectedHostKey)
}

func parseSSHHostKey(offer Offer) (gossh.PublicKey, error) {
	expectedHostKey, err := gossh.ParsePublicKey(offer.SSHHostKey)
	if err != nil {
		return nil, errors.New("host sent an invalid SSH host key")
	}
	return expectedHostKey, nil
}

func runSSHSession(ctx context.Context, config ClientConfig, offer Offer, connection net.Conn, expectedHostKey gossh.PublicKey) (bool, error) {
	sshConfig := &gossh.ClientConfig{
		User:              "croc",
		HostKeyAlgorithms: []string{expectedHostKey.Type()},
		HostKeyCallback: func(_ string, _ net.Addr, actual gossh.PublicKey) error {
			if !bytes.Equal(actual.Marshal(), expectedHostKey.Marshal()) {
				return errors.New("SSH host key does not match authenticated invitation")
			}
			return nil
		},
	}
	clientConnection, channels, requests, err := gossh.NewClientConn(connection, "croc-ssh", sshConfig)
	if err != nil {
		return false, fmt.Errorf("SSH handshake: %w", err)
	}
	client := gossh.NewClient(clientConnection, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return false, fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	size := normalizeWindowSize(config.InitialSize)
	if config.Terminal != nil && term.IsTerminal(int(config.Terminal.Fd())) {
		if width, height, sizeErr := term.GetSize(int(config.Terminal.Fd())); sizeErr == nil {
			size = normalizeWindowSize(WindowSize{Width: width, Height: height})
		}
	}
	terminalName := os.Getenv("TERM")
	if terminalName == "" {
		terminalName = "xterm-256color"
	}
	if err = session.RequestPty(terminalName, size.Height, size.Width, gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return false, fmt.Errorf("request SSH terminal: %w", err)
	}

	input := config.inputBroker.activate()
	defer config.inputBroker.deactivate(input)
	session.Stdin = input
	session.Stdout = config.Output
	session.Stderr = config.ErrorOutput

	var restore func()
	if config.Terminal != nil && term.IsTerminal(int(config.Terminal.Fd())) {
		state, rawErr := term.MakeRaw(int(config.Terminal.Fd()))
		if rawErr != nil {
			return false, fmt.Errorf("make local terminal raw: %w", rawErr)
		}
		restore = func() { _ = term.Restore(int(config.Terminal.Fd()), state) }
		defer restore()
	}
	if config.OnEvent != nil {
		config.OnEvent(JoinEvent{State: "connected", Role: offer.Role, Transport: offer.Transport})
	}
	if err = session.Shell(); err != nil {
		return false, fmt.Errorf("start shared SSH shell: %w", err)
	}
	stopResize := watchWindowChanges(ctx, config.Terminal, session)
	defer stopResize()

	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()
	select {
	case err = <-wait:
		if config.inputBroker.Detached() {
			return true, nil
		}
		return true, err
	case <-ctx.Done():
		return true, ctx.Err()
	}
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
