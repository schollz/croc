//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

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
	protocolVersion      = 1
	authTimeout          = 30 * time.Second
	rendezvousLease      = 5 * time.Minute
	maxSSHControlMessage = 1 << 20
	maxPAKEPayload       = 4 << 10
	maxSSHHostKey        = 16 << 10
	maxTailcatAddress    = 512 << 10
	readWritePort        = uint16(22)
	readOnlyPort         = uint16(23)
)

// sshOffer is returned to an authenticated guest. The Tailcat address and SSH
// host key are authenticated by the PAKE channel, so the embedded SSH client
// can pin both without a trust-on-first-use prompt.
type sshOffer struct {
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
		return readWritePort, nil
	case RoleReadOnly:
		return readOnlyPort, nil
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
func guestPAKE(c *comm.Comm, components codephrase.SSHComponents, curve string) ([]byte, time.Time, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, time.Time{}, err
	}
	deadline := time.Now().Add(authTimeout)
	initiator, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 0, curve,
		pakekey.PurposeSSH, components.RoomName,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	initiatorBytes := append([]byte(nil), initiator.Bytes()...)
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: initiatorBytes, Bytes2: []byte(curve),
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}

	response, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return nil, time.Time{}, err
	}
	if response.Type != message.TypePAKE || response.Version != pakekey.ProtocolVersion {
		return nil, time.Time{}, errors.New("invalid SSH PAKE response")
	}
	if len(response.Bytes) == 0 || len(response.Bytes) > maxPAKEPayload {
		return nil, time.Time{}, fmt.Errorf("invalid SSH PAKE response length %d", len(response.Bytes))
	}
	if len(response.Bytes2) != pakekey.SaltSize {
		return nil, time.Time{}, fmt.Errorf("invalid SSH PAKE salt length %d", len(response.Bytes2))
	}
	if err = initiator.Update(response.Bytes); err != nil {
		return nil, time.Time{}, fmt.Errorf("SSH PAKE response: %w", err)
	}
	keys, err := deriveSSHKeys(
		initiator, components, curve, initiatorBytes, response.Bytes, response.Bytes2,
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
		return nil, time.Time{}, errors.New("SSH host failed PAKE key confirmation")
	}
	return keys.EncryptionKey, deadline, nil
}

// hostPAKE authenticates the host as PAKE party B and returns a transcript-
// bound traffic key after mutual key confirmation.
func hostPAKE(c *comm.Comm, components codephrase.SSHComponents) ([]byte, time.Time, error) {
	if err := validateRendezvous(c); err != nil {
		return nil, time.Time{}, err
	}
	request, err := receiveMessageUntil(c, nil, time.Now().Add(rendezvousLease))
	if err != nil {
		return nil, time.Time{}, err
	}
	deadline := time.Now().Add(authTimeout)
	if request.Type != message.TypePAKE || request.Version != pakekey.ProtocolVersion {
		return nil, time.Time{}, errors.New("invalid SSH PAKE request")
	}
	if len(request.Bytes) == 0 || len(request.Bytes) > maxPAKEPayload {
		return nil, time.Time{}, fmt.Errorf("invalid SSH PAKE request length %d", len(request.Bytes))
	}
	if len(request.Bytes2) == 0 || len(request.Bytes2) > 64 {
		return nil, time.Time{}, errors.New("invalid SSH PAKE curve")
	}
	curve := string(request.Bytes2)
	responder, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 1, curve,
		pakekey.PurposeSSH, components.RoomName,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = responder.Update(request.Bytes); err != nil {
		return nil, time.Time{}, fmt.Errorf("SSH PAKE request: %w", err)
	}
	responderBytes := append([]byte(nil), responder.Bytes()...)
	salt := make([]byte, pakekey.SaltSize)
	if _, err = rand.Read(salt); err != nil {
		return nil, time.Time{}, fmt.Errorf("generate SSH PAKE salt: %w", err)
	}
	keys, err := deriveSSHKeys(
		responder, components, curve, request.Bytes, responderBytes, salt,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: responderBytes, Bytes2: salt,
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}
	confirmation, err := receiveMessageUntil(c, nil, deadline)
	if err != nil {
		return nil, time.Time{}, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		confirmation.Version != pakekey.ProtocolVersion ||
		!pakekey.Confirm(keys.ConfirmationA, confirmation.Bytes) {
		return nil, time.Time{}, errors.New("SSH guest failed PAKE key confirmation")
	}
	if err = sendMessageUntil(c, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationB,
	}, deadline); err != nil {
		return nil, time.Time{}, err
	}
	return keys.EncryptionKey, deadline, nil
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

func receiveMessageUntil(c *comm.Comm, encryptionKey []byte, deadline time.Time) (message.Message, error) {
	for {
		b, err := c.ReceiveWithDeadlineLimit(deadline, maxSSHControlMessage)
		if err != nil {
			return message.Message{}, err
		}
		// A relay sends this framed byte once per second while this side is
		// waiting for its peer to join the room. It is transport-level liveness,
		// not part of the authenticated SSH-share protocol.
		if bytes.Equal(b, []byte{1}) {
			continue
		}
		m, err := message.DecodeWithLimit(encryptionKey, b, maxSSHControlMessage)
		if err != nil {
			return message.Message{}, err
		}
		return m, nil
	}
}

func sendMessageUntil(c *comm.Comm, encryptionKey []byte, outgoing message.Message, deadline time.Time) error {
	if err := c.Connection().SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set SSH control write deadline: %w", err)
	}
	return message.Send(c, encryptionKey, outgoing)
}

func sendAuthorizationRequest(c *comm.Comm, encryptionKey []byte, client key.NodePublic, transport Transport, deadline time.Time) error {
	if err := validateTransport(transport); err != nil {
		return err
	}
	if client.IsZero() {
		return errors.New("SSH client key is required")
	}
	return sendMessageUntil(c, encryptionKey, message.Message{
		Type:    message.TypeSSHAuthorize,
		Version: protocolVersion,
		Bytes:   client.AppendTo(nil),
		Features: []string{
			string(transport),
		},
	}, deadline)
}

func receiveAuthorizationRequest(c *comm.Comm, encryptionKey []byte, deadline time.Time) (authorizationRequest, error) {
	m, err := receiveMessageUntil(c, encryptionKey, deadline)
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
	if request.clientKey.IsZero() {
		return authorizationRequest{}, errors.New("SSH client key is required")
	}
	if err := validateTransport(request.transport); err != nil {
		return authorizationRequest{}, err
	}
	return request, nil
}

func sendOffer(c *comm.Comm, encryptionKey []byte, offer sshOffer, deadline time.Time) error {
	if err := validateRole(offer.Role); err != nil {
		return err
	}
	if err := validateTransport(offer.Transport); err != nil {
		return err
	}
	wantPort, _ := offer.Role.port()
	if offer.TailcatAddress == "" || len(offer.TailcatAddress) > maxTailcatAddress ||
		len(offer.SSHHostKey) == 0 || len(offer.SSHHostKey) > maxSSHHostKey || offer.Port == 0 {
		return errors.New("SSH offer is incomplete")
	}
	if offer.Port != wantPort {
		return errors.New("SSH offer role and port are inconsistent")
	}
	return sendMessageUntil(c, encryptionKey, message.Message{
		Type:     message.TypeSSHOffer,
		Version:  protocolVersion,
		Message:  offer.TailcatAddress,
		Bytes:    offer.SSHHostKey,
		Num:      int(offer.Port),
		Features: []string{string(offer.Role), string(offer.Transport)},
	}, deadline)
}

func receiveOffer(c *comm.Comm, encryptionKey []byte, deadline time.Time) (sshOffer, error) {
	m, err := receiveMessageUntil(c, encryptionKey, deadline)
	if err != nil {
		return sshOffer{}, err
	}
	if m.Type != message.TypeSSHOffer || m.Version != protocolVersion || len(m.Features) != 2 {
		return sshOffer{}, errors.New("invalid SSH offer")
	}
	if m.Num <= 0 || m.Num > 65535 {
		return sshOffer{}, fmt.Errorf("invalid SSH offer port %d", m.Num)
	}
	offer := sshOffer{
		TailcatAddress: m.Message,
		SSHHostKey:     append([]byte(nil), m.Bytes...),
		Port:           uint16(m.Num),
		Role:           Role(m.Features[0]),
		Transport:      Transport(m.Features[1]),
	}
	if err := validateRole(offer.Role); err != nil {
		return sshOffer{}, err
	}
	if err := validateTransport(offer.Transport); err != nil {
		return sshOffer{}, err
	}
	wantPort, _ := offer.Role.port()
	if offer.TailcatAddress == "" || len(offer.TailcatAddress) > maxTailcatAddress ||
		len(offer.SSHHostKey) == 0 || len(offer.SSHHostKey) > maxSSHHostKey || offer.Port != wantPort {
		return sshOffer{}, errors.New("SSH offer is incomplete or inconsistent")
	}
	return offer, nil
}
