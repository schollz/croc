//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/compress"
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
	clientAuth := []byte("01234567890123456789012345678901")
	wantOffer := sshOffer{
		TailcatAddress: "tc-test-address",
		SSHHostKey:     []byte("ssh-host-key"),
		Port:           23,
		Role:           RoleReadOnly,
		Transport:      TransportRelay,
	}
	hostErr := make(chan error, 1)
	go func() {
		encryptionKey, deadline, err := hostPAKE(host, components)
		if err != nil {
			hostErr <- err
			return
		}
		request, err := receiveAuthorizationRequest(host, encryptionKey, deadline)
		if err == nil && request.clientKey != clientKey.Public() {
			err = errClientKeyMismatch
		}
		if err == nil && request.transport != TransportRelay {
			err = &testError{"transport mismatch"}
		}
		if err == nil && !bytes.Equal(request.clientAuth, clientAuth) {
			err = &testError{"client authentication mismatch"}
		}
		if err == nil {
			err = sendOffer(host, encryptionKey, wantOffer, deadline)
		}
		if err == nil {
			_, err = host.Connection().Write([]byte("raw-relay-stream"))
		}
		hostErr <- err
	}()

	encryptionKey, deadline, err := guestPAKE(guest, components, "p256")
	require.NoError(t, err)
	require.NoError(t, sendAuthorizationRequest(guest, encryptionKey, clientKey.Public(), clientAuth, TransportRelay, deadline))
	gotOffer, err := receiveOffer(guest, encryptionKey, deadline)
	require.NoError(t, err)
	require.Equal(t, wantOffer, gotOffer)
	raw := make([]byte, len("raw-relay-stream"))
	_, err = io.ReadFull(guest.Connection(), raw)
	require.NoError(t, err)
	require.Equal(t, "raw-relay-stream", string(raw))
	require.NoError(t, <-hostErr)
}

func TestGuestPAKEAdvertisesSSHRendezvous(t *testing.T) {
	components, err := codephrase.ParseSSH("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	guestDone := make(chan error, 1)
	go func() {
		_, _, pakeErr := guestPAKE(guest, components, "p256")
		guestDone <- pakeErr
	}()

	payload, err := host.ReceiveWithDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)
	request, err := message.Decode(nil, payload)
	require.NoError(t, err)
	require.Equal(t, message.TypePAKE, request.Type)
	require.Contains(t, request.Features, message.FeatureSSHRendezvous)
	host.Close()
	require.Error(t, <-guestDone)
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

	got, err := receiveMessageUntil(host, nil, time.Now().Add(time.Second))
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
		_, _, hostPAKEErr := hostPAKE(host, hostComponents)
		hostErr <- hostPAKEErr
	}()
	_, _, guestErr := guestPAKE(guest, guestComponents, "p256")
	require.Error(t, guestErr)
	require.Error(t, <-hostErr)
}

func TestOfferRejectsRolePortEscalation(t *testing.T) {
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	t.Cleanup(host.Close)
	t.Cleanup(func() { _ = guestConn.Close() })
	keyBytes := []byte("01234567890123456789012345678901")

	err := sendOffer(host, keyBytes, sshOffer{
		TailcatAddress: "tc-test",
		SSHHostKey:     []byte("key"),
		Port:           22,
		Role:           RoleReadOnly,
		Transport:      TransportTailcat,
	}, time.Now().Add(time.Second))
	require.Error(t, err)
}

func TestReceiveMessageKeepalivesDoNotExtendDeadline(t *testing.T) {
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	go func() {
		for guest.Send([]byte{1}) == nil {
		}
	}()
	started := time.Now()
	_, err := receiveMessageUntil(host, nil, time.Now().Add(50*time.Millisecond))
	var networkError net.Error
	require.ErrorAs(t, err, &networkError)
	require.True(t, networkError.Timeout())
	require.Less(t, time.Since(started), time.Second)
}

func TestReceiveMessageBoundsDecompressedControlPayload(t *testing.T) {
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	encoded, err := message.Encode(nil, message.Message{
		Type:    message.TypeSSHOffer,
		Message: strings.Repeat("a", maxSSHControlMessage+1),
	})
	require.NoError(t, err)
	require.Less(t, len(encoded), maxSSHControlMessage)
	go func() { _ = guest.Send(encoded) }()
	_, err = receiveMessageUntil(host, nil, time.Now().Add(time.Second))
	require.ErrorIs(t, err, compress.ErrDecompressedSizeExceeded)
}

func TestHostPAKERejectsOversizedWireValue(t *testing.T) {
	components, err := codephrase.ParseSSH("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	hostConn, guestConn := net.Pipe()
	host := comm.New(hostConn)
	guest := comm.New(guestConn)
	t.Cleanup(host.Close)
	t.Cleanup(guest.Close)

	go func() {
		_ = message.Send(guest, nil, message.Message{
			Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
			Bytes: make([]byte, maxPAKEPayload+1), Bytes2: []byte("p256"),
		})
	}()
	_, _, err = hostPAKE(host, components)
	require.ErrorContains(t, err, "PAKE request length")
}

func TestAuthorizationRejectsZeroClientKey(t *testing.T) {
	err := sendAuthorizationRequest(nil, nil, key.NodePublic{}, make([]byte, sshClientAuthSize), TransportTailcat, time.Now().Add(time.Second))
	require.ErrorContains(t, err, "client key is required")
}

func TestAuthorizationRejectsMissingClientAuthentication(t *testing.T) {
	err := sendAuthorizationRequest(nil, nil, key.NewNode().Public(), nil, TransportRelay, time.Now().Add(time.Second))
	require.ErrorContains(t, err, "client authentication length")
}

func TestAuthorizationRejectsZeroClientAuthentication(t *testing.T) {
	err := sendAuthorizationRequest(nil, nil, key.NewNode().Public(), make([]byte, sshClientAuthSize), TransportRelay, time.Now().Add(time.Second))
	require.ErrorContains(t, err, "client authentication is required")
}
