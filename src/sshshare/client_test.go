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
	var events []string
	err := Join(t.Context(), ClientConfig{
		Code:            "acid-acorn-acre-acts-ahead-alien",
		RelayAddress:    "relay.example:9009",
		RelayPassword:   "pass",
		Input:           bytes.NewReader(nil),
		Reconnect:       true,
		ReconnectWindow: time.Second,
		OnEvent: func(event JoinEvent) {
			events = append(events, event.State)
		},
		request: func(_ context.Context, _, _ string, _ codephrase.SSHComponents, _ string, transport Transport) (Offer, key.NodePrivate, *comm.Comm, error) {
			requests++
			transports = append(transports, transport)
			return Offer{Role: RoleReadWrite, Transport: transport}, key.NewNode(), newTestControl(t), nil
		},
		attach: func(context.Context, ClientConfig, Offer, key.NodePrivate) (bool, error) {
			tailcatAttachments++
			if tailcatAttachments == 1 {
				return true, io.EOF
			}
			return true, nil
		},
		attachRelay: func(context.Context, ClientConfig, Offer, net.Conn) (bool, error) {
			relayAttachments++
			return true, io.EOF
		},
	})
	require.NoError(t, err)
	require.Equal(t, 3, requests)
	require.Equal(t, []Transport{TransportTailcat, TransportRelay, TransportTailcat}, transports)
	require.Equal(t, 2, tailcatAttachments)
	require.Equal(t, 1, relayAttachments)
	require.Contains(t, events, "reconnecting")
}

func TestJoinFallsBackToCrocRelayBeforeFirstAttachment(t *testing.T) {
	var transports []Transport
	err := Join(t.Context(), ClientConfig{
		Code:          "acid-acorn-acre-acts-ahead-alien",
		RelayAddress:  "relay.example:9009",
		RelayPassword: "pass",
		Input:         bytes.NewReader(nil),
		Reconnect:     false,
		request: func(_ context.Context, _, _ string, _ codephrase.SSHComponents, _ string, transport Transport) (Offer, key.NodePrivate, *comm.Comm, error) {
			transports = append(transports, transport)
			return Offer{Role: RoleReadOnly, Transport: transport}, key.NewNode(), newTestControl(t), nil
		},
		attach: func(context.Context, ClientConfig, Offer, key.NodePrivate) (bool, error) {
			return false, errors.New("Tailcat unavailable")
		},
		attachRelay: func(context.Context, ClientConfig, Offer, net.Conn) (bool, error) {
			return true, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, []Transport{TransportTailcat, TransportRelay}, transports)
}

func newTestControl(t *testing.T) *comm.Comm {
	t.Helper()
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	return comm.New(client)
}
