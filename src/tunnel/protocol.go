// Package tunnel shares a single local service through authenticated croc peers.
package tunnel

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/pake/v3"
)

const (
	protocolVersion   = 1
	authTimeout       = 30 * time.Second
	rendezvousLease   = 5 * time.Minute
	maxControl        = 64 << 10
	maxPAKEPayload    = 4 << 10
	rendezvousFeature = "tunnel-rendezvous-v1"
	channelTCP        = "croc-tcp-v1"
	ChannelHTTP       = "croc-http-v1"
	ChannelWebSocket  = "croc-websocket-v1"
	endedRequest      = "croc-tunnel-ended-v1"
	MaxChannels       = 64
	MaxGuests         = 16
)

// ErrRendezvousBusy means two guests met while the host was reopening its room.
var ErrRendezvousBusy = errors.New("tunnel rendezvous busy")

func validateRendezvous(c *comm.Comm) error {
	if c == nil || c.Connection() == nil {
		return errors.New("missing tunnel connection")
	}
	return nil
}
func guestPAKE(c *comm.Comm, components codephrase.TunnelComponents, curve string) ([]byte, time.Time, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, time.Time{}, err
	}
	deadline := time.Now().Add(authTimeout)
	initiator, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 0, curve,
		pakekey.PurposeTunnel, components.RoomName,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	initiatorBytes := append([]byte(nil), initiator.Bytes()...)
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: initiatorBytes, Bytes2: []byte(curve), Message: "tunnel-guest-v1",
		Features: []string{rendezvousFeature},
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}

	response, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return nil, time.Time{}, err
	}
	if response.Type == message.TypePAKE && response.Message == "tunnel-guest-v1" {
		return nil, time.Time{}, ErrRendezvousBusy
	}
	if response.Type != message.TypePAKE || response.Version != pakekey.ProtocolVersion || response.Message != "tunnel-host-v1" {
		return nil, time.Time{}, errors.New("invalid tunnel PAKE response")
	}
	if !hasFeature(response.Features, rendezvousFeature) {
		return nil, time.Time{}, errors.New("tunnel host did not advertise tunnel rendezvous support")
	}
	if len(response.Bytes) == 0 || len(response.Bytes) > maxPAKEPayload {
		return nil, time.Time{}, fmt.Errorf("invalid tunnel PAKE response length %d", len(response.Bytes))
	}
	if len(response.Bytes2) != pakekey.SaltSize {
		return nil, time.Time{}, fmt.Errorf("invalid tunnel PAKE salt length %d", len(response.Bytes2))
	}
	if err = initiator.Update(response.Bytes); err != nil {
		return nil, time.Time{}, fmt.Errorf("tunnel PAKE response: %w", err)
	}
	keys, err := derivePeerKeys(
		initiator, components, curve, pakekey.PurposeTunnel, initiatorBytes, response.Bytes, response.Bytes2,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationA,
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}
	confirmation, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return nil, time.Time{}, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		confirmation.Version != pakekey.ProtocolVersion ||
		!pakekey.Confirm(keys.ConfirmationB, confirmation.Bytes) {
		return nil, time.Time{}, errors.New("tunnel host failed PAKE key confirmation")
	}
	return keys.EncryptionKey, deadline, nil
}

// hostPAKE authenticates the host as PAKE party B and returns a transcript-
// bound traffic key after mutual key confirmation.
func hostPAKE(c *comm.Comm, components codephrase.TunnelComponents) ([]byte, time.Time, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, time.Time{}, err
	}
	request, err := receiveMessageUntil(c, nil, time.Now().Add(rendezvousLease))
	if err != nil {
		return nil, time.Time{}, err
	}
	deadline := time.Now().Add(authTimeout)
	if request.Type != message.TypePAKE || request.Version != pakekey.ProtocolVersion || request.Message != "tunnel-guest-v1" {
		return nil, time.Time{}, errors.New("invalid tunnel PAKE request")
	}
	if len(request.Bytes) == 0 || len(request.Bytes) > maxPAKEPayload {
		return nil, time.Time{}, fmt.Errorf("invalid tunnel PAKE request length %d", len(request.Bytes))
	}
	if len(request.Bytes2) == 0 || len(request.Bytes2) > 64 {
		return nil, time.Time{}, errors.New("invalid tunnel PAKE curve")
	}
	purpose := pakekey.PurposeTunnel
	if !hasFeature(request.Features, rendezvousFeature) {
		return nil, time.Time{}, errors.New("peer does not support tunnel sharing")
	}
	curve := string(request.Bytes2)
	responder, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 1, curve,
		purpose, components.RoomName,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = responder.Update(request.Bytes); err != nil {
		return nil, time.Time{}, fmt.Errorf("tunnel PAKE request: %w", err)
	}
	responderBytes := append([]byte(nil), responder.Bytes()...)
	salt := make([]byte, pakekey.SaltSize)
	if _, err = rand.Read(salt); err != nil {
		return nil, time.Time{}, fmt.Errorf("generate tunnel PAKE salt: %w", err)
	}
	keys, err := derivePeerKeys(
		responder, components, curve, purpose, request.Bytes, responderBytes, salt,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	response := message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion, Message: "tunnel-host-v1",
		Bytes: responderBytes, Bytes2: salt,
	}
	response.Features = []string{rendezvousFeature}
	if err = sendMessageUntil(c, nil, response, deadline); err != nil {
		return nil, time.Time{}, err
	}
	confirmation, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return nil, time.Time{}, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		confirmation.Version != pakekey.ProtocolVersion ||
		!pakekey.Confirm(keys.ConfirmationA, confirmation.Bytes) {
		return nil, time.Time{}, errors.New("tunnel guest failed PAKE key confirmation")
	}
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationB,
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}
	return keys.EncryptionKey, deadline, nil
}

func derivePeerKeys(
	p *pake.Pake,
	components codephrase.TunnelComponents,
	curve, purpose string,
	initiator, responder, salt []byte,
) (pakekey.Keys, error) {
	shared, err := p.SessionKey()
	if err != nil {
		return pakekey.Keys{}, err
	}
	return pakekey.Derive(shared, pakekey.Context{
		Purpose:   purpose,
		Room:      components.RoomName,
		Curve:     curve,
		Initiator: initiator,
		Responder: responder,
		Salt:      salt,
	})
}

func hasFeature(features []string, expected string) bool {
	return slices.Contains(features, expected)
}

func receiveMessageUntil(c *comm.Comm, encryptionKey []byte, deadline time.Time) (message.Message, error) {
	for {
		b, err := c.ReceiveWithDeadlineLimit(deadline, maxControl)
		if err != nil {
			return message.Message{}, err
		}
		// A relay sends this framed byte once per second while this side is
		// waiting for its peer to join the room. It is transport-level liveness,
		// not part of the authenticated tunnel-share protocol.
		if bytes.Equal(b, []byte{1}) {
			continue
		}
		m, err := message.DecodeWithLimit(encryptionKey, b, maxControl)
		if err != nil {
			return message.Message{}, err
		}
		return m, nil
	}
}

func sendMessageUntil(c *comm.Comm, encryptionKey []byte, outgoing message.Message, deadline time.Time) error {
	if err := c.Connection().SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set tunnel control write deadline: %w", err)
	}
	return message.Send(c, encryptionKey, outgoing)
}

// Consume relay waiting-room pings before handing the stream to SSH.
func prepareStream(connection net.Conn) error {
	c := comm.New(connection)
	deadline := time.Now().Add(authTimeout)
	if err := sendMessageUntil(c, nil, message.Message{Type: "tunnel-stream-v1"}, deadline); err != nil {
		return err
	}
	peer, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return err
	}
	if peer.Type != "tunnel-stream-v1" {
		return errors.New("invalid tunnel stream")
	}
	return connection.SetDeadline(time.Time{})
}
