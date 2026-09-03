//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"io"
	"net"
	"testing"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/key"
)

func TestSSHPAKEAndAuthorizationRoundTrip(t *testing.T) {
	components, err := codephrase.ParseSSH("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	clientKey := key.NewNode()
	wantOffer := Offer{
		TailcatAddress: "tc-test-address",
		SSHHostKey:     []byte("ssh-host-key"),
		Port:           23,
		Role:           RoleReadOnly,
		Transport:      TransportRelay,
	}
	hostErr := make(chan error, 1)
	go func() {
		encryptionKey, err := hostPAKE(host, components)
		if err != nil {
			hostErr <- err
			return
		}
		request, err := receiveAuthorizationRequest(host, encryptionKey)
		if err == nil && request.clientKey != clientKey.Public() {
			err = errClientKeyMismatch
		}
		if err == nil && request.transport != TransportRelay {
			err = &testError{"transport mismatch"}
		}
		if err == nil {
			err = sendOffer(host, encryptionKey, wantOffer)
		}
		if err == nil {
			_, err = host.Connection().Write([]byte("raw-relay-stream"))
		}
		hostErr <- err
	}()

	encryptionKey, err := guestPAKE(guest, components, "p256")
	require.NoError(t, err)
	require.NoError(t, sendAuthorizationRequest(guest, encryptionKey, clientKey.Public(), TransportRelay))
	gotOffer, err := receiveOffer(guest, encryptionKey)
	require.NoError(t, err)
	require.Equal(t, wantOffer, gotOffer)
	raw := make([]byte, len("raw-relay-stream"))
	_, err = io.ReadFull(guest.Connection(), raw)
	require.NoError(t, err)
	require.Equal(t, "raw-relay-stream", string(raw))
	require.NoError(t, <-hostErr)
}

func TestReceiveMessageIgnoresRelayKeepalive(t *testing.T) {
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	sendErr := make(chan error, 1)
	go func() {
		if err := guest.Send([]byte{1}); err != nil {
			sendErr <- err
			return
		}
		sendErr <- message.Send(guest, nil, message.Message{
			Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		})
	}()

	got, err := receiveMessage(host, nil)
	require.NoError(t, err)
	require.Equal(t, message.TypePAKE, got.Type)
	require.NoError(t, <-sendErr)
}

var errClientKeyMismatch = &testError{"client key mismatch"}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

func TestSSHPAKERejectsWrongCode(t *testing.T) {
	hostComponents, err := codephrase.ParseSSH("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	guestComponents, err := codephrase.ParseSSH("acid-acorn-acre-acts-ahead-apron")
	require.NoError(t, err)
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	hostErr := make(chan error, 1)
	go func() {
		defer host.Close()
		_, hostPAKEErr := hostPAKE(host, hostComponents)
		hostErr <- hostPAKEErr
	}()
	_, guestErr := guestPAKE(guest, guestComponents, "p256")
	require.Error(t, guestErr)
	require.Error(t, <-hostErr)
}

func TestOfferRejectsRolePortEscalation(t *testing.T) {
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	t.Cleanup(host.Close)
	t.Cleanup(func() { _ = guestConn.Close() })
	keyBytes := []byte("01234567890123456789012345678901")

	err := sendOffer(host, keyBytes, Offer{
		TailcatAddress: "tc-test",
		SSHHostKey:     []byte("key"),
		Port:           22,
		Role:           RoleReadOnly,
		Transport:      TransportTailcat,
	})
	require.Error(t, err)
}
