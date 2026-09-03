//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/key"
)

func TestDetachReaderStopsAtControlBracket(t *testing.T) {
	reader := &detachReader{reader: bytes.NewBuffer([]byte("hello\x1dignored"))}
	buffer := make([]byte, 32)
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "hello", string(buffer[:n]))
	require.True(t, reader.Detached())
	n, err = reader.Read(buffer)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestDetachReaderImmediate(t *testing.T) {
	reader := &detachReader{reader: bytes.NewBuffer([]byte("\x1d"))}
	buffer := make([]byte, 1)
	n, err := reader.Read(buffer)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestDetachReaderStopsHostAtControlC(t *testing.T) {
	reader := &detachReader{reader: bytes.NewBuffer([]byte("before\x03ignored"))}
	buffer := make([]byte, 32)
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "before", string(buffer[:n]))
	require.True(t, reader.Stopped())
	require.False(t, reader.Detached())
	n, err = reader.Read(buffer)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestDetachReaderImmediateControlC(t *testing.T) {
	reader := &detachReader{reader: bytes.NewBuffer([]byte("\x03"))}
	buffer := make([]byte, 1)
	n, err := reader.Read(buffer)
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, reader.Stopped())
}

func TestJoinReconnectsAfterEOFWithFreshAuthorization(t *testing.T) {
	requests := 0
	tailcatAttachments := 0
	relayAttachments := 0
	var transports []Transport
	var clientKeys []key.NodePublic
	var events []JoinState
	config := ClientConfig{
		Code:            "acid-acorn-acre-acts-ahead-alien",
		RelayAddress:    "relay.example:9009",
		RelayPassword:   "pass",
		Input:           bytes.NewReader(nil),
		Reconnect:       true,
		ReconnectWindow: time.Second,
		OnEvent: func(event JoinEvent) {
			events = append(events, event.State)
		},
	}
	err := joinWithDeps(t.Context(), config, clientDeps{
		request: func(_ context.Context, _, _ string, _ codephrase.SSHComponents, _ string, clientKey key.NodePrivate, transport Transport) (authorization, error) {
			requests++
			transports = append(transports, transport)
			clientKeys = append(clientKeys, clientKey.Public())
			return authorization{offer: sshOffer{Role: RoleReadWrite, Transport: transport}, control: newTestControl(t)}, nil
		},
		attach: func(context.Context, clientSessionConfig, sshOffer, key.NodePrivate) (bool, error) {
			tailcatAttachments++
			if tailcatAttachments == 1 {
				return true, io.EOF
			}
			return true, nil
		},
		attachRelay: func(context.Context, clientSessionConfig, sshOffer, net.Conn) (bool, error) {
			relayAttachments++
			return true, io.EOF
		},
	})
	require.NoError(t, err)
	require.Equal(t, 3, requests)
	require.Equal(t, []Transport{TransportTailcat, TransportRelay, TransportTailcat}, transports)
	require.Equal(t, 2, tailcatAttachments)
	require.Equal(t, 1, relayAttachments)
	require.Len(t, clientKeys, 3)
	require.Equal(t, clientKeys[0], clientKeys[1])
	require.Equal(t, clientKeys[0], clientKeys[2])
	require.Contains(t, events, JoinStateReconnecting)
}

func TestJoinFallsBackToCrocRelayBeforeFirstAttachment(t *testing.T) {
	var transports []Transport
	config := ClientConfig{
		Code:          "acid-acorn-acre-acts-ahead-alien",
		RelayAddress:  "relay.example:9009",
		RelayPassword: "pass",
		Input:         bytes.NewReader(nil),
		Reconnect:     false,
	}
	err := joinWithDeps(t.Context(), config, clientDeps{
		request: func(_ context.Context, _, _ string, _ codephrase.SSHComponents, _ string, clientKey key.NodePrivate, transport Transport) (authorization, error) {
			transports = append(transports, transport)
			return authorization{offer: sshOffer{Role: RoleReadOnly, Transport: transport}, control: newTestControl(t)}, nil
		},
		attach: func(context.Context, clientSessionConfig, sshOffer, key.NodePrivate) (bool, error) {
			return false, errors.New("Tailcat unavailable")
		},
		attachRelay: func(context.Context, clientSessionConfig, sshOffer, net.Conn) (bool, error) {
			return true, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, []Transport{TransportTailcat, TransportRelay}, transports)
}

func TestAttachRelaySSHUsesPinnedHostKey(t *testing.T) {
	pty := newMemoryPTY()
	hub := newTerminalHub(t.Context(), pty, nil, nil)
	t.Cleanup(func() { _ = hub.Close() })
	signer := newTestSigner(t)
	server := newSharedSSHServer(hub, signer, RoleReadOnly, nil)
	t.Cleanup(func() { _ = server.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	serverDone := make(chan struct{})
	go func() {
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			server.HandleConn(serverConn)
		}
		close(serverDone)
	}()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	events := make(chan JoinEvent, 1)
	input := bytes.NewReader(nil)
	connected, err := attachRelaySSH(t.Context(), clientSessionConfig{
		config: ClientConfig{
			Input: input, Output: io.Discard, ErrorOutput: io.Discard,
			OnEvent: func(event JoinEvent) { events <- event },
		},
		input: newInputBroker(input),
	}, sshOffer{
		SSHHostKey: signer.PublicKey().Marshal(),
		Port:       readOnlyPort,
		Role:       RoleReadOnly,
		Transport:  TransportRelay,
	}, clientConn)
	require.NoError(t, err)
	require.True(t, connected)
	require.Equal(t, JoinStateConnected, (<-events).State)
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("SSH server connection did not close")
	}
}

func TestRunSSHSessionCancellationInterruptsHandshake(t *testing.T) {
	signer := newTestSigner(t)
	clientConn, silentPeer := net.Pipe()
	t.Cleanup(func() { _ = silentPeer.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	input := bytes.NewReader(nil)
	started := time.Now()
	connected, err := runSSHSession(ctx, clientSessionConfig{
		config: ClientConfig{Input: input, Output: io.Discard, ErrorOutput: io.Discard},
		input:  newInputBroker(input),
	}, sshOffer{}, clientConn, signer.PublicKey())
	require.False(t, connected)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
}

func newTestControl(t *testing.T) *comm.Comm {
	t.Helper()
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	return comm.New(client)
}
