//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

// Package sshshare implements croc's authenticated, reconnectable SSH sharing
// protocol. A croc relay provides rendezvous and PAKE; the authenticated SSH
// stream then uses Tailcat's direct-or-DERP path, with that same ordinary croc
// relay available as the data fallback.
package sshshare

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/pake/v3"
	go4mem "go4.org/mem"
	"tailscale.com/types/key"
)

const (
	protocolVersion = 1
	authTimeout     = 30 * time.Second
)

// Role is the authority granted by an SSH invitation.
type Role string

const (
	RoleReadWrite Role = "read-write"
	RoleReadOnly  Role = "read-only"
)

// Transport identifies the terminal data path selected after PAKE.
type Transport string

const (
	TransportTailcat Transport = "tailcat"
	TransportRelay   Transport = "relay"
)

// Offer is returned to an authenticated guest. The Tailcat address and SSH
// host key are authenticated by the PAKE channel, so the embedded SSH client
// can pin both without a trust-on-first-use prompt.
type Offer struct {
	TailcatAddress string
	SSHHostKey     []byte
	Port           uint16
	Role           Role
	Transport      Transport
}

type authorizationRequest struct {
	clientKey key.NodePublic
	transport Transport
}

func (r Role) port() (uint16, error) {
	switch r {
	case RoleReadWrite:
		return 22, nil
	case RoleReadOnly:
		return 23, nil
	default:
		return 0, fmt.Errorf("unsupported SSH access role %q", r)
	}
}

func validateRole(r Role) error {
	_, err := r.port()
	return err
}

func validateTransport(transport Transport) error {
	switch transport {
	case TransportTailcat, TransportRelay:
		return nil
	default:
		return fmt.Errorf("unsupported SSH transport %q", transport)
	}
}

func validateRendezvous(c *comm.Comm) error {
	if c == nil || c.Connection() == nil {
		return errors.New("SSH rendezvous connection is nil")
	}
	return nil
}

// guestPAKE authenticates a guest as PAKE party A and returns a transcript-
// bound traffic key after mutual key confirmation.
func guestPAKE(c *comm.Comm, components codephrase.SSHComponents, curve string) ([]byte, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, err
	}
	initiator, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 0, curve,
		pakekey.PurposeSSH, components.RoomName,
	)
	if err != nil {
		return nil, err
	}
	initiatorBytes := append([]byte(nil), initiator.Bytes()...)
	if err = message.Send(c, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: initiatorBytes, Bytes2: []byte(curve),
	}); err != nil {
		return nil, err
	}

	response, err := receiveMessage(c, nil)
	if err != nil {
		return nil, err
	}
	if response.Type != message.TypePAKE || response.Version != pakekey.ProtocolVersion {
		return nil, errors.New("invalid SSH PAKE response")
	}
	if len(response.Bytes2) != pakekey.SaltSize {
		return nil, fmt.Errorf("invalid SSH PAKE salt length %d", len(response.Bytes2))
	}
	if err = initiator.Update(response.Bytes); err != nil {
		return nil, fmt.Errorf("SSH PAKE response: %w", err)
	}
	keys, err := deriveSSHKeys(
		initiator, components, curve, initiatorBytes, response.Bytes, response.Bytes2,
	)
	if err != nil {
		return nil, err
	}
	if err = message.Send(c, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationA,
	}); err != nil {
		return nil, err
	}
	confirmation, err := receiveMessage(c, nil)
	if err != nil {
		return nil, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		confirmation.Version != pakekey.ProtocolVersion ||
		!pakekey.Confirm(keys.ConfirmationB, confirmation.Bytes) {
		return nil, errors.New("SSH host failed PAKE key confirmation")
	}
	return keys.EncryptionKey, nil
}

// hostPAKE authenticates the host as PAKE party B and returns a transcript-
// bound traffic key after mutual key confirmation.
func hostPAKE(c *comm.Comm, components codephrase.SSHComponents) ([]byte, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, err
	}
	request, err := receiveMessage(c, nil)
	if err != nil {
		return nil, err
	}
	if request.Type != message.TypePAKE || request.Version != pakekey.ProtocolVersion {
		return nil, errors.New("invalid SSH PAKE request")
	}
	curve := string(request.Bytes2)
	responder, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 1, curve,
		pakekey.PurposeSSH, components.RoomName,
	)
	if err != nil {
		return nil, err
	}
	if err = responder.Update(request.Bytes); err != nil {
		return nil, fmt.Errorf("SSH PAKE request: %w", err)
	}
	responderBytes := append([]byte(nil), responder.Bytes()...)
	salt := make([]byte, pakekey.SaltSize)
	if _, err = rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate SSH PAKE salt: %w", err)
	}
	keys, err := deriveSSHKeys(
		responder, components, curve, request.Bytes, responderBytes, salt,
	)
	if err != nil {
		return nil, err
	}
	if err = message.Send(c, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: responderBytes, Bytes2: salt,
	}); err != nil {
		return nil, err
	}
	confirmation, err := receiveMessage(c, nil)
	if err != nil {
		return nil, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		confirmation.Version != pakekey.ProtocolVersion ||
		!pakekey.Confirm(keys.ConfirmationA, confirmation.Bytes) {
		return nil, errors.New("SSH guest failed PAKE key confirmation")
	}
	if err = message.Send(c, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationB,
	}); err != nil {
		return nil, err
	}
	return keys.EncryptionKey, nil
}

func deriveSSHKeys(
	p *pake.Pake,
	components codephrase.SSHComponents,
	curve string,
	initiator, responder, salt []byte,
) (pakekey.Keys, error) {
	shared, err := p.SessionKey()
	if err != nil {
		return pakekey.Keys{}, err
	}
	return pakekey.Derive(shared, pakekey.Context{
		Purpose:   pakekey.PurposeSSH,
		Room:      components.RoomName,
		Curve:     curve,
		Initiator: initiator,
		Responder: responder,
		Salt:      salt,
	})
}

func receiveMessage(c *comm.Comm, encryptionKey []byte) (message.Message, error) {
	for {
		// The deadline rolls forward on relay keepalives while this side is
		// waiting alone in a room. Once a peer joins and keepalives stop, an
		// incomplete or malicious authentication cannot occupy the listener
		// indefinitely.
		b, err := c.ReceiveWithDeadline(time.Now().Add(authTimeout))
		if err != nil {
			return message.Message{}, err
		}
		// A relay sends this framed byte once per second while this side is
		// waiting for its peer to join the room. It is transport-level liveness,
		// not part of the authenticated SSH-share protocol.
		if bytes.Equal(b, []byte{1}) {
			continue
		}
		m, err := message.Decode(encryptionKey, b)
		if err != nil {
			return message.Message{}, err
		}
		return m, nil
	}
}

func sendAuthorizationRequest(c *comm.Comm, encryptionKey []byte, client key.NodePublic, transport Transport) error {
	if err := validateTransport(transport); err != nil {
		return err
	}
	return message.Send(c, encryptionKey, message.Message{
		Type:    message.TypeSSHAuthorize,
		Version: protocolVersion,
		Bytes:   client.AppendTo(nil),
		Features: []string{
			string(transport),
		},
	})
}

func receiveAuthorizationRequest(c *comm.Comm, encryptionKey []byte) (authorizationRequest, error) {
	m, err := receiveMessage(c, encryptionKey)
	if err != nil {
		return authorizationRequest{}, err
	}
	if m.Type != message.TypeSSHAuthorize || m.Version != protocolVersion || len(m.Features) != 1 {
		return authorizationRequest{}, errors.New("invalid SSH authorization request")
	}
	if len(m.Bytes) != 32 {
		return authorizationRequest{}, fmt.Errorf("invalid SSH client key length %d", len(m.Bytes))
	}
	request := authorizationRequest{
		clientKey: key.NodePublicFromRaw32(go4mem.B(m.Bytes)),
		transport: Transport(m.Features[0]),
	}
	if err := validateTransport(request.transport); err != nil {
		return authorizationRequest{}, err
	}
	return request, nil
}

func sendOffer(c *comm.Comm, encryptionKey []byte, offer Offer) error {
	if err := validateRole(offer.Role); err != nil {
		return err
	}
	if err := validateTransport(offer.Transport); err != nil {
		return err
	}
	wantPort, _ := offer.Role.port()
	if offer.TailcatAddress == "" || len(offer.SSHHostKey) == 0 || offer.Port == 0 {
		return errors.New("SSH offer is incomplete")
	}
	if offer.Port != wantPort {
		return errors.New("SSH offer role and port are inconsistent")
	}
	return message.Send(c, encryptionKey, message.Message{
		Type:     message.TypeSSHOffer,
		Version:  protocolVersion,
		Message:  offer.TailcatAddress,
		Bytes:    offer.SSHHostKey,
		Num:      int(offer.Port),
		Features: []string{string(offer.Role), string(offer.Transport)},
	})
}

func receiveOffer(c *comm.Comm, encryptionKey []byte) (Offer, error) {
	m, err := receiveMessage(c, encryptionKey)
	if err != nil {
		return Offer{}, err
	}
	if m.Type != message.TypeSSHOffer || m.Version != protocolVersion || len(m.Features) != 2 {
		return Offer{}, errors.New("invalid SSH offer")
	}
	if m.Num <= 0 || m.Num > 65535 {
		return Offer{}, fmt.Errorf("invalid SSH offer port %d", m.Num)
	}
	offer := Offer{
		TailcatAddress: m.Message,
		SSHHostKey:     append([]byte(nil), m.Bytes...),
		Port:           uint16(m.Num),
		Role:           Role(m.Features[0]),
		Transport:      Transport(m.Features[1]),
	}
	if err := validateRole(offer.Role); err != nil {
		return Offer{}, err
	}
	if err := validateTransport(offer.Transport); err != nil {
		return Offer{}, err
	}
	wantPort, _ := offer.Role.port()
	if offer.TailcatAddress == "" || len(offer.SSHHostKey) == 0 || offer.Port != wantPort {
		return Offer{}, errors.New("SSH offer is incomplete or inconsistent")
	}
	return offer, nil
}
