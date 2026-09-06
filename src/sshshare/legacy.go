//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
)

const (
	legacyHandshakeRequest = "handshake"
	legacyIPRequest        = "ips?"
	maxLegacyDataPorts     = 64
	relayConnectTimeout    = 10 * time.Second
)

type legacyProbeMessage struct {
	Bytes   []byte
	Bytes2  []byte
	Kind    string
	Version int
	Curve   string
}

func (h *Host) serveLegacyRendezvous(invite *legacyInvitation) {
	defer h.wg.Done()
	failures := 0
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		session, err := h.deps.connect(
			h.ctx, invite.relay, h.config.RelayPassword, invite.components.RoomName, relayConnectTimeout,
		)
		if err == nil {
			if session.connection == nil {
				err = errors.New("legacy rendezvous connection is nil")
			} else {
				stopClose := context.AfterFunc(h.ctx, session.connection.Close)
				err = h.notifyLegacyClient(session, invite)
				session.connection.Close()
				stopClose()
			}
		}
		if errors.Is(err, errLegacyClientNotified) {
			err = nil
		}
		if err != nil && h.ctx.Err() == nil && h.config.Logf != nil {
			h.config.Logf("SSH %s legacy rendezvous: %v", invite.role, err)
		}
		if err == nil {
			failures = 0
		} else {
			failures++
		}
		timer := time.NewTimer(rendezvousRetryDelay(failures, err))
		select {
		case <-timer.C:
		case <-h.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (h *Host) notifyLegacyClient(session relaySession, invite *legacyInvitation) error {
	connection := session.connection
	preambleDeadline := time.Now().Add(rendezvousLease)
	first, err := receiveLegacyFrameUntil(connection, preambleDeadline)
	if err != nil {
		return err
	}
	if !bytes.Equal(first, []byte(legacyHandshakeRequest)) {
		if err = answerLegacyLocalProbe(connection, invite.components.RoomName, invite.components.PAKEPassphrase, first, time.Now().Add(authTimeout)); err != nil {
			return err
		}
		first, err = receiveLegacyFrameUntil(connection, preambleDeadline)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(first, []byte(legacyHandshakeRequest)) {
		return errors.New("invalid legacy handshake request")
	}

	request, err := receiveMessageUntil(connection, nil, preambleDeadline)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(authTimeout)
	encryptionKey, deadline, err := respondHostPAKE(
		connection,
		pakeCredentials{roomName: invite.components.RoomName, passphrase: invite.components.PAKEPassphrase},
		request,
		pakekey.PurposeTransfer,
		nil,
		deadline,
	)
	if err != nil {
		return err
	}

	// Pre-SSH receivers synchronously pair every advertised data room after key
	// confirmation. Pair them before sending the error so the client can return
	// to its control-message loop and display it.
	dataConnections, err := h.openLegacyDataChannels(session, invite, deadline)
	if err != nil {
		return err
	}
	defer closeLegacyConnections(dataConnections)

	externalIP, err := receiveMessageUntil(connection, encryptionKey, deadline)
	if err != nil {
		return err
	}
	if externalIP.Type != message.TypeExternalIP {
		return fmt.Errorf("expected legacy external IP, got message type %q", externalIP.Type)
	}
	if err = sendMessageUntil(connection, encryptionKey, message.Message{
		Type: message.TypeError, Message: legacyClientMessage,
	}, deadline); err != nil {
		return err
	}
	return errLegacyClientNotified
}

func answerLegacyLocalProbe(connection *comm.Comm, room, passphrase string, payload []byte, deadline time.Time) error {
	var request legacyProbeMessage
	if err := json.Unmarshal(payload, &request); err != nil || request.Kind != "pake1" {
		return errors.New("invalid legacy local probe")
	}
	if request.Version != pakekey.ProtocolVersion {
		return fmt.Errorf("unsupported legacy local PAKE version %d", request.Version)
	}
	if len(request.Bytes) == 0 || len(request.Bytes) > maxPAKEPayload {
		return fmt.Errorf("invalid legacy local PAKE request length %d", len(request.Bytes))
	}
	if len(request.Curve) == 0 || len(request.Curve) > 64 {
		return errors.New("invalid legacy local PAKE curve")
	}
	responder, err := pakekey.Init(
		[]byte(passphrase), 1, request.Curve, pakekey.PurposeLocalProbe, room,
	)
	if err != nil {
		return err
	}
	if err = responder.Update(request.Bytes); err != nil {
		return fmt.Errorf("legacy local PAKE request: %w", err)
	}
	responderBytes := append([]byte(nil), responder.Bytes()...)
	salt := make([]byte, pakekey.SaltSize)
	if _, err = rand.Read(salt); err != nil {
		return fmt.Errorf("generate legacy local PAKE salt: %w", err)
	}
	keys, err := derivePeerKeys(
		responder, room, request.Curve, pakekey.PurposeLocalProbe,
		request.Bytes, responderBytes, salt,
	)
	if err != nil {
		return err
	}
	response, err := json.Marshal(legacyProbeMessage{
		Bytes: responderBytes, Bytes2: salt, Kind: "pake2",
		Version: pakekey.ProtocolVersion, Curve: request.Curve,
	})
	if err != nil {
		return err
	}
	if err = sendLegacyFrameUntil(connection, response, deadline); err != nil {
		return err
	}
	encryptedRequest, err := receiveLegacyFrameUntil(connection, deadline)
	if err != nil {
		return err
	}
	decryptedRequest, err := crypt.Decrypt(encryptedRequest, keys.EncryptionKey)
	if err != nil {
		return err
	}
	if !bytes.Equal(decryptedRequest, []byte(legacyIPRequest)) {
		return errors.New("invalid legacy local IP request")
	}
	// A protocol-correct empty list keeps the old client on the relay without
	// advertising a fake local endpoint.
	emptyIPs, err := json.Marshal([]string{})
	if err != nil {
		return err
	}
	encryptedResponse, err := crypt.Encrypt(emptyIPs, keys.EncryptionKey)
	if err != nil {
		return err
	}
	return sendLegacyFrameUntil(connection, encryptedResponse, deadline)
}

func receiveLegacyFrameUntil(connection *comm.Comm, deadline time.Time) ([]byte, error) {
	for {
		payload, err := connection.ReceiveWithDeadlineLimit(deadline, maxSSHControlMessage)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(payload, []byte{1}) {
			continue
		}
		return payload, nil
	}
}

func sendLegacyFrameUntil(connection *comm.Comm, payload []byte, deadline time.Time) error {
	if err := connection.Connection().SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set legacy control write deadline: %w", err)
	}
	return connection.Send(payload)
}

func (h *Host) openLegacyDataChannels(session relaySession, invite *legacyInvitation, deadline time.Time) ([]*comm.Comm, error) {
	ports := strings.Split(session.banner, ",")
	if len(ports) == 0 || len(ports) > maxLegacyDataPorts {
		return nil, fmt.Errorf("invalid legacy relay data port count %d", len(ports))
	}
	host, _, err := net.SplitHostPort(invite.relay)
	if err != nil {
		return nil, fmt.Errorf("invalid legacy relay address %q: %w", invite.relay, err)
	}
	for _, port := range ports {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return nil, fmt.Errorf("invalid legacy relay data port %q", port)
		}
	}

	ctx, cancel := context.WithDeadline(h.ctx, deadline)
	defer cancel()
	type result struct {
		index      int
		connection *comm.Comm
		err        error
	}
	results := make(chan result, len(ports))
	var wg sync.WaitGroup
	for index, port := range ports {
		wg.Add(1)
		go func() {
			defer wg.Done()
			address := net.JoinHostPort(host, port)
			room := fmt.Sprintf("%s-%d", invite.components.RoomName, index)
			connection, connectErr := h.deps.connectData(
				ctx, address, h.config.RelayPassword, room, session.capability, relayConnectTimeout,
			)
			results <- result{index: index, connection: connection, err: connectErr}
		}()
	}
	wg.Wait()
	close(results)

	connections := make([]*comm.Comm, len(ports))
	for result := range results {
		if result.err != nil {
			err = errors.Join(err, result.err)
			continue
		}
		if result.connection == nil {
			err = errors.Join(err, errors.New("legacy relay data connection is nil"))
			continue
		}
		connections[result.index] = result.connection
	}
	if err != nil {
		closeLegacyConnections(connections)
		return nil, err
	}
	return connections, nil
}

func closeLegacyConnections(connections []*comm.Comm) {
	for _, connection := range connections {
		if connection != nil {
			connection.Close()
		}
	}
}
