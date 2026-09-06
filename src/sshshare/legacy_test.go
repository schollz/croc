//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/stretchr/testify/require"
)

func TestLegacyClientReceivesEncryptedUpgradeError(t *testing.T) {
	for _, localProbe := range []bool{false, true} {
		t.Run(fmt.Sprintf("local-probe-%t", localProbe), func(t *testing.T) {
			code := "acid-acorn-acre-acts-ahead-alien"
			components, err := codephrase.Parse(code)
			require.NoError(t, err)
			hostRaw, clientRaw := net.Pipe()
			hostConnection, clientConnection := comm.New(hostRaw), comm.New(clientRaw)
			t.Cleanup(hostConnection.Close)
			t.Cleanup(clientConnection.Close)

			var callsMu sync.Mutex
			var dataCalls []string
			var dataPeers []net.Conn
			host := &Host{
				ctx:    t.Context(),
				config: HostConfig{RelayPassword: "relay-password"},
				deps: hostDeps{connectData: func(_ context.Context, address, password, room, capability string, _ time.Duration) (*comm.Comm, error) {
					if password != "relay-password" {
						return nil, errors.New("relay password mismatch")
					}
					hostData, peerData := net.Pipe()
					callsMu.Lock()
					dataCalls = append(dataCalls, address+"|"+room+"|"+capability)
					dataPeers = append(dataPeers, peerData)
					callsMu.Unlock()
					return comm.New(hostData), nil
				}}.withDefaults(),
			}
			t.Cleanup(func() {
				for _, peer := range dataPeers {
					_ = peer.Close()
				}
			})
			invite := &legacyInvitation{
				role: RoleReadWrite, components: components, relay: "relay.example:9009",
			}
			hostErr := make(chan error, 1)
			go func() {
				defer hostConnection.Close()
				hostErr <- host.notifyLegacyClient(relaySession{
					connection: hostConnection,
					banner:     "9101,9102",
					capability: "relay-capability",
				}, invite)
			}()

			upgrade, err := legacyClientExchange(clientConnection, components, localProbe, nil)
			require.NoError(t, err)
			require.Equal(t, message.TypeError, upgrade.Type)
			require.Equal(t, legacyClientMessage, upgrade.Message)
			require.ErrorIs(t, <-hostErr, errLegacyClientNotified)

			callsMu.Lock()
			sort.Strings(dataCalls)
			require.Equal(t, []string{
				"relay.example:9101|" + components.RoomName + "-0|relay-capability",
				"relay.example:9102|" + components.RoomName + "-1|relay-capability",
			}, dataCalls)
			callsMu.Unlock()
		})
	}
}

func TestLegacyClientWithWrongSecretGetsNoUpgradeMessage(t *testing.T) {
	hostComponents, err := codephrase.Parse("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	clientComponents, err := codephrase.Parse("acid-acorn-acre-acts-ahead-apron")
	require.NoError(t, err)
	require.Equal(t, hostComponents.RoomName, clientComponents.RoomName)
	require.NotEqual(t, hostComponents.PAKEPassphrase, clientComponents.PAKEPassphrase)

	hostRaw, clientRaw := net.Pipe()
	hostConnection, clientConnection := comm.New(hostRaw), comm.New(clientRaw)
	t.Cleanup(hostConnection.Close)
	t.Cleanup(clientConnection.Close)
	host := &Host{ctx: t.Context(), deps: hostDeps{}.withDefaults()}
	hostErr := make(chan error, 1)
	go func() {
		defer hostConnection.Close()
		hostErr <- host.notifyLegacyClient(relaySession{connection: hostConnection}, &legacyInvitation{
			components: hostComponents, relay: "relay.example:9009",
		})
	}()

	_, err = legacyClientExchange(clientConnection, clientComponents, false, nil)
	require.Error(t, err)
	require.NotErrorIs(t, <-hostErr, errLegacyClientNotified)
}

func TestMalformedLegacyClientGetsNoResponse(t *testing.T) {
	components, err := codephrase.Parse("acid-acorn-acre-acts-ahead-alien")
	require.NoError(t, err)
	hostRaw, clientRaw := net.Pipe()
	hostConnection, clientConnection := comm.New(hostRaw), comm.New(clientRaw)
	t.Cleanup(hostConnection.Close)
	t.Cleanup(clientConnection.Close)
	host := &Host{ctx: t.Context(), deps: hostDeps{}.withDefaults()}
	hostErr := make(chan error, 1)
	go func() {
		defer hostConnection.Close()
		hostErr <- host.notifyLegacyClient(relaySession{connection: hostConnection}, &legacyInvitation{
			components: components, relay: "relay.example:9009",
		})
	}()
	require.NoError(t, clientConnection.Send([]byte(legacyHandshakeRequest)))
	require.NoError(t, clientConnection.Send([]byte("not-a-croc-message")))
	_, err = clientConnection.ReceiveWithDeadline(time.Now().Add(time.Second))
	require.Error(t, err)
	require.Error(t, <-hostErr)
}

func TestLegacyClientUpgradeThroughLocalRelay(t *testing.T) {
	controlPort, dataPort := freeLegacyTestPort(t), freeLegacyTestPort(t)
	controlAddress := net.JoinHostPort("127.0.0.1", controlPort)
	dataAddress := net.JoinHostPort("127.0.0.1", dataPort)
	capabilities, err := tcp.NewRelayCapabilitySet(1)
	require.NoError(t, err)
	go func() {
		_ = tcp.RunWithOptionsAsync("127.0.0.1", dataPort, "relay-password",
			tcp.WithCtx(t.Context()), tcp.WithFastAdmission(capabilities))
	}()
	go func() {
		_ = tcp.RunWithOptionsAsync("127.0.0.1", controlPort, "relay-password",
			tcp.WithCtx(t.Context()), tcp.WithBanner(dataPort), tcp.WithFastAdmission(capabilities))
	}()
	waitForLegacyTestRelay(t, dataAddress)
	waitForLegacyTestRelay(t, controlAddress)

	var eventsMu sync.Mutex
	var events []HostEvent
	host, err := startHostWithDeps(t.Context(), HostConfig{
		ReadWriteCode: "acid-acorn-acre-acts-ahead-alien",
		ReadOnlyCode:  "bacon-acorn-acre-acts-ahead-alien",
		RelayAddress:  controlAddress,
		RelayPassword: "relay-password",
		OnEvent: func(event HostEvent) {
			eventsMu.Lock()
			events = append(events, event)
			eventsMu.Unlock()
		},
	}, hostDeps{
		startTerminal: func(ctx context.Context, _ []string, _ string, _ WindowSize) (*terminalHub, error) {
			return newTerminalHub(ctx, newMemoryPTY(), nil, nil), nil
		},
		startTransport: func(context.Context, HostConfig, func(uint16) func(net.Conn)) (hostTransport, string, error) {
			return &tailcat.Server{}, "tailcat-offer", nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, host.Close()) })

	for _, role := range []Role{RoleReadWrite, RoleReadOnly} {
		components, parseErr := codephrase.Parse(host.Code(role))
		require.NoError(t, parseErr)
		control, banner, _, connectErr := tcp.ConnectToTCPServer(
			controlAddress, "relay-password", components.RoomName, time.Second,
		)
		require.NoError(t, connectErr)
		openData := func() ([]*comm.Comm, error) {
			return openLegacyClientDataChannels(controlAddress, banner, "relay-password", components.RoomName)
		}
		upgrade, exchangeErr := legacyClientExchange(control, components, true, openData)
		control.Close()
		require.NoError(t, exchangeErr)
		require.Equal(t, message.TypeError, upgrade.Type)
		require.Equal(t, legacyClientMessage, upgrade.Message)
	}

	eventsMu.Lock()
	require.Empty(t, events)
	eventsMu.Unlock()
	host.grantMu.Lock()
	require.Empty(t, host.grants)
	host.grantMu.Unlock()
}

func legacyClientExchange(
	connection *comm.Comm,
	components codephrase.Components,
	localProbe bool,
	openData func() ([]*comm.Comm, error),
) (message.Message, error) {
	deadline := time.Now().Add(5 * time.Second)
	if localProbe {
		if err := legacyClientLocalProbe(connection, components, deadline); err != nil {
			return message.Message{}, err
		}
	}
	if err := sendLegacyFrameUntil(connection, []byte(legacyHandshakeRequest), deadline); err != nil {
		return message.Message{}, err
	}
	initiator, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 0, "p256", pakekey.PurposeTransfer, components.RoomName,
	)
	if err != nil {
		return message.Message{}, err
	}
	initiatorBytes := append([]byte(nil), initiator.Bytes()...)
	if err = message.Send(connection, nil, message.Message{
		Type: message.TypePAKE, Version: pakekey.ProtocolVersion,
		Bytes: initiatorBytes, Bytes2: []byte("p256"),
	}); err != nil {
		return message.Message{}, err
	}
	response, err := receiveMessageUntil(connection, nil, deadline)
	if err != nil {
		return message.Message{}, err
	}
	if response.Type != message.TypePAKE || response.Version != pakekey.ProtocolVersion {
		return message.Message{}, errors.New("invalid legacy PAKE response")
	}
	if err = initiator.Update(response.Bytes); err != nil {
		return message.Message{}, err
	}
	keys, err := derivePeerKeys(
		initiator, components.RoomName, "p256", pakekey.PurposeTransfer,
		initiatorBytes, response.Bytes, response.Bytes2,
	)
	if err != nil {
		return message.Message{}, err
	}
	if err = message.Send(connection, nil, message.Message{
		Type: message.TypePAKEConfirm, Version: pakekey.ProtocolVersion,
		Bytes: keys.ConfirmationA,
	}); err != nil {
		return message.Message{}, err
	}
	confirmation, err := receiveMessageUntil(connection, nil, deadline)
	if err != nil {
		return message.Message{}, err
	}
	if confirmation.Type != message.TypePAKEConfirm ||
		!pakekey.Confirm(keys.ConfirmationB, confirmation.Bytes) {
		return message.Message{}, errors.New("legacy host PAKE confirmation failed")
	}

	var dataConnections []*comm.Comm
	if openData != nil {
		dataConnections, err = openData()
		if err != nil {
			return message.Message{}, err
		}
		defer closeLegacyConnections(dataConnections)
	}
	if err = message.Send(connection, keys.EncryptionKey, message.Message{
		Type: message.TypeExternalIP, Message: "192.0.2.10", Bytes: response.Bytes,
	}); err != nil {
		return message.Message{}, err
	}
	return receiveMessageUntil(connection, keys.EncryptionKey, deadline)
}

func legacyClientLocalProbe(connection *comm.Comm, components codephrase.Components, deadline time.Time) error {
	initiator, err := pakekey.Init(
		[]byte(components.PAKEPassphrase), 0, "p256", pakekey.PurposeLocalProbe, components.RoomName,
	)
	if err != nil {
		return err
	}
	initiatorBytes := append([]byte(nil), initiator.Bytes()...)
	request, err := json.Marshal(legacyProbeMessage{
		Bytes: initiatorBytes, Kind: "pake1", Version: pakekey.ProtocolVersion, Curve: "p256",
	})
	if err != nil {
		return err
	}
	if err = sendLegacyFrameUntil(connection, request, deadline); err != nil {
		return err
	}
	payload, err := receiveLegacyFrameUntil(connection, deadline)
	if err != nil {
		return err
	}
	var response legacyProbeMessage
	if err = json.Unmarshal(payload, &response); err != nil || response.Kind != "pake2" {
		return errors.New("invalid legacy local PAKE response")
	}
	if err = initiator.Update(response.Bytes); err != nil {
		return err
	}
	keys, err := derivePeerKeys(
		initiator, components.RoomName, "p256", pakekey.PurposeLocalProbe,
		initiatorBytes, response.Bytes, response.Bytes2,
	)
	if err != nil {
		return err
	}
	encryptedRequest, err := crypt.Encrypt([]byte(legacyIPRequest), keys.EncryptionKey)
	if err != nil {
		return err
	}
	if err = sendLegacyFrameUntil(connection, encryptedRequest, deadline); err != nil {
		return err
	}
	encryptedResponse, err := receiveLegacyFrameUntil(connection, deadline)
	if err != nil {
		return err
	}
	responseBytes, err := crypt.Decrypt(encryptedResponse, keys.EncryptionKey)
	if err != nil {
		return err
	}
	var ips []string
	if err = json.Unmarshal(responseBytes, &ips); err != nil {
		return err
	}
	if len(ips) != 0 {
		return fmt.Errorf("legacy local probe returned endpoints: %v", ips)
	}
	return nil
}

func openLegacyClientDataChannels(controlAddress, banner, password, room string) ([]*comm.Comm, error) {
	host, _, err := net.SplitHostPort(controlAddress)
	if err != nil {
		return nil, err
	}
	ports := stringsSplitNonempty(banner)
	type result struct {
		index      int
		connection *comm.Comm
		err        error
	}
	results := make(chan result, len(ports))
	for index, port := range ports {
		go func() {
			connection, _, _, connectErr := tcp.ConnectToTCPServer(
				net.JoinHostPort(host, port), password, fmt.Sprintf("%s-%d", room, index), time.Second,
			)
			results <- result{index: index, connection: connection, err: connectErr}
		}()
	}
	connections := make([]*comm.Comm, len(ports))
	for range ports {
		result := <-results
		if result.err != nil {
			closeLegacyConnections(connections)
			return nil, result.err
		}
		connections[result.index] = result.connection
	}
	return connections, nil
}

func stringsSplitNonempty(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func freeLegacyTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	require.NoError(t, listener.Close())
	return port
}

func waitForLegacyTestRelay(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := tcp.PingServer(address); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("relay %s did not start", address)
}
