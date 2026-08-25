package croc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/derptransport"
	"github.com/schollz/croc/v11/src/message"
	"github.com/shayne/derphole/pkg/derpbind"
	"github.com/shayne/derphole/pkg/token"
)

type fakeDERPListener struct {
	tokenValue string
	conn       net.Conn
	err        error
	closed     atomic.Bool
}

var experimentalDERPTokenSequence atomic.Uint32

type pairedDERPListener struct {
	tokenValue  string
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   atomic.Bool
}

func (l *pairedDERPListener) Token() string { return l.tokenValue }
func (l *pairedDERPListener) Accept(ctx context.Context) (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (l *pairedDERPListener) Close() error {
	if l.closeOnce.CompareAndSwap(false, true) {
		close(l.closed)
	}
	return nil
}

func (l *fakeDERPListener) Token() string { return l.tokenValue }
func (l *fakeDERPListener) Accept(context.Context) (net.Conn, error) {
	return l.conn, l.err
}
func (l *fakeDERPListener) Close() error {
	l.closed.Store(true)
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}

func experimentalDERPTestToken(t *testing.T) string {
	return experimentalDERPToken(t, token.CapabilityAttach)
}

func experimentalDERPGroupTestToken(t *testing.T) string {
	return experimentalDERPToken(t, token.CapabilityAttach|token.CapabilityAttachGroup)
}

func experimentalDERPToken(t *testing.T, capabilities uint32) string {
	t.Helper()
	sequence := experimentalDERPTokenSequence.Add(1)
	var sessionID [16]byte
	sessionID[0] = byte(sequence)
	sessionID[1] = byte(sequence >> 8)
	sessionID[2] = byte(sequence >> 16)
	sessionID[3] = byte(sequence >> 24)
	encoded, err := token.Encode(token.Token{
		SessionID:       sessionID,
		ExpiresUnix:     time.Now().Add(time.Minute).Unix(),
		BootstrapRegion: 1,
		Capabilities:    capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newExperimentalDERPTestClient(t *testing.T, sender bool, transports ...derpDataTransport) *Client {
	t.Helper()
	transport := derpDataTransport(newFakeDERPDataTransport(false))
	if len(transports) > 0 {
		transport = transports[0]
	}
	client, err := newClient(Options{
		SharedSecret: "correct-horse-battery",
		IsSender:     sender,
		Transport:    TransportDERP,
		Curve:        "p256",
		RelayAddress: "invalid relay address that must not be dialed",
		RelayPorts:   []string{"1", "2", "3", "4"},
	}, transport)
	if err != nil {
		t.Fatal(err)
	}
	client.peerDERP = true
	client.Key = bytes.Repeat([]byte{0x42}, 32)
	return client
}

func newDERPAttempt(control *comm.Comm) *transferAttemptState {
	return &transferAttemptState{
		errc:          make(chan error, 1),
		control:       control,
		derpSetupDone: make(chan struct{}),
	}
}

func TestTransportOptionIsRemembered(t *testing.T) {
	encoded, err := json.Marshal(Options{Transport: TransportDERP})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"Transport":"derp"`) {
		t.Fatalf("remembered options omit transport: %s", encoded)
	}
}

func TestExperimentalDERPNewRejectsUnsupportedModesAndRoute(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	for _, test := range []struct {
		name    string
		options Options
		want    string
	}{
		{name: "local", options: Options{OnlyLocal: true}, want: "--local"},
		{name: "qrcode", options: Options{ShowQrCode: true}, want: "--qrcode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.options.SharedSecret = "experimental-derp-conflict"
			test.options.Transport = TransportDERP
			if _, err := newClient(test.options, transport); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want %q", err, test.want)
			}
		})
	}
	t.Run("custom DERP route", func(t *testing.T) {
		t.Setenv(derpbind.CustomDERPServerEnv, "https://derp.example")
		_, err := newClient(Options{SharedSecret: "experimental-derp-route", Transport: TransportDERP}, transport)
		if !errors.Is(err, ErrDERPConnection) || !errors.Is(err, derptransport.ErrCustomRoute) {
			t.Fatalf("New() error = %v", err)
		}
	})
	t.Run("auto defers custom route to fallback", func(t *testing.T) {
		t.Setenv(derpbind.CustomDERPServerEnv, "https://derp.example")
		if _, err := newClient(Options{SharedSecret: "auto-derp-route", Transport: TransportAuto, Curve: "p256"}, transport); err != nil {
			t.Fatalf("New() rejected auto transport: %v", err)
		}
	})
}

func TestUnavailableDERPPlatformUsesRelayInAutoAndRejectsStrict(t *testing.T) {
	transport := &fakeDERPDataTransport{}
	auto, err := newClient(Options{SharedSecret: "unsupported-auto-transport", Transport: TransportAuto, Curve: "p256"}, transport)
	if err != nil {
		t.Fatalf("auto transport rejected: %v", err)
	}
	if features := auto.pakeFeatures(); len(features) != 0 {
		t.Fatalf("unsupported auto advertised DERP: %v", features)
	}
	_, err = newClient(Options{SharedSecret: "unsupported-strict-transport", Transport: TransportDERP}, transport)
	if !errors.Is(err, ErrDERPConnection) || !errors.Is(err, derptransport.ErrUnsupported) {
		t.Fatalf("strict unsupported error = %v", err)
	}
}

func TestTransportPolicyFeaturesAndStrictMismatch(t *testing.T) {
	legacyTransport := newFakeDERPDataTransport(false)
	strict := &Client{
		Options:       Options{IsSender: true, Transport: TransportDERP},
		stop:          newStop(context.Background()),
		derpTransport: legacyTransport,
	}
	if err := strict.activateSecureChannel(newDERPAttempt(nil)); err == nil || !strings.Contains(err.Error(), "DERP-capable peer") {
		t.Fatalf("strict mismatch error = %v", err)
	}

	autoFeatures := (&Client{Options: Options{Transport: TransportAuto}, derpTransport: legacyTransport}).pakeFeatures()
	if !supportsFeature(autoFeatures, derpFeature) || !supportsFeature(autoFeatures, derpNegotiationFeature) || supportsFeature(autoFeatures, derpRequiredFeature) {
		t.Fatalf("auto PAKE features = %v", autoFeatures)
	}
	if supportsFeature(autoFeatures, derpAttachGroupFeature) {
		t.Fatalf("unvalidated AttachGroup feature was advertised: %v", autoFeatures)
	}
	groupTransport := newFakeDERPDataTransport(true)
	if gatedFeatures := (&Client{Options: Options{Transport: TransportAuto}, derpTransport: groupTransport}).pakeFeatures(); !supportsFeature(gatedFeatures, derpAttachGroupFeature) {
		t.Fatalf("validated AttachGroup feature was not advertised: %v", gatedFeatures)
	}
	strictFeatures := strict.pakeFeatures()
	if !supportsFeature(strictFeatures, derpRequiredFeature) {
		t.Fatalf("strict PAKE features = %v", strictFeatures)
	}
	if got := (&Client{Options: Options{Transport: TransportRelay}, derpTransport: groupTransport}).pakeFeatures(); len(got) != 0 {
		t.Fatalf("relay PAKE features = %v", got)
	}
	legacyStrictPeer := &Client{
		Options:       Options{Transport: TransportRelay},
		peerDERP:      true,
		stop:          newStop(context.Background()),
		derpTransport: legacyTransport,
	}
	if err := legacyStrictPeer.activateSecureChannel(newDERPAttempt(nil)); err == nil || !strings.Contains(err.Error(), "peer requires DERP") {
		t.Fatalf("legacy strict peer mismatch error = %v", err)
	}
}

func TestAutoTransportSkipsDERPForIncapablePeer(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	var listenCalls atomic.Int32
	transport.listen = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
		listenCalls.Add(1)
		return nil, errors.New("must not be called")
	}
	client := &Client{
		Options: Options{
			IsSender:     true,
			Transport:    TransportAuto,
			RelayAddress: "127.0.0.1:1",
			RelayPorts:   []string{"1"},
		},
		stop:          newStop(context.Background()),
		derpTransport: transport,
	}
	err := client.activateSecureChannel(newDERPAttempt(nil))
	if !errors.Is(err, ErrRelayConnection) {
		t.Fatalf("relay selection error = %v", err)
	}
	if listenCalls.Load() != 0 {
		t.Fatalf("DERP listener called %d times", listenCalls.Load())
	}
}

func TestRelayFallbackClosesPendingDERPConnection(t *testing.T) {
	client := &Client{
		Options: Options{
			Transport:    TransportAuto,
			RelayAddress: "127.0.0.1:1",
			RelayPorts:   []string{"1"},
		},
		peerDERP:            true,
		peerDERPNegotiation: true,
		stop:                newStop(context.Background()),
	}
	attempt := newDERPAttempt(nil)
	local, peer := net.Pipe()
	defer peer.Close()
	canceled := atomic.Bool{}
	if err := attempt.setDERPPending(newSingleDERPBundle(local), func() { canceled.Store(true) }); err != nil {
		t.Fatal(err)
	}
	err := client.processTransportSelect(message.Message{
		Type:    message.TypeTransportSelect,
		Message: string(TransportRelay),
	}, attempt)
	if !errors.Is(err, ErrRelayConnection) {
		t.Fatalf("relay activation error = %v", err)
	}
	if !canceled.Load() {
		t.Fatal("pending DERP context was not canceled")
	}
	if _, writeErr := peer.Write([]byte("closed")); writeErr == nil {
		t.Fatal("pending DERP connection remained open")
	}
}

func TestExperimentalDERPSenderInstallsOneDataConnection(t *testing.T) {
	// A new gated binary must retain legacy Attach when the peer did not
	// advertise AttachGroup.
	transport := newFakeDERPDataTransport(true)
	client := newExperimentalDERPTestClient(t, true, transport)
	controlLocal, controlPeer := net.Pipe()
	defer controlPeer.Close()
	client.conn[0] = comm.New(controlLocal)
	dataLocal, dataPeer := net.Pipe()
	defer dataPeer.Close()
	tokenValue := experimentalDERPTestToken(t)
	listener := &fakeDERPListener{tokenValue: tokenValue, conn: dataLocal}
	var listenCalls atomic.Int32
	transport.listen = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
		listenCalls.Add(1)
		return listener, nil
	}

	offerResult := make(chan message.Message, 1)
	go func() {
		payload, receiveErr := comm.New(controlPeer).Receive()
		if receiveErr != nil {
			offerResult <- message.Message{Message: receiveErr.Error()}
			return
		}
		offer, decodeErr := message.Decode(client.Key, payload)
		if decodeErr != nil {
			offerResult <- message.Message{Message: decodeErr.Error()}
			return
		}
		offerResult <- offer
	}()

	attempt := newDERPAttempt(client.conn[0])
	if err := client.activateSecureChannel(attempt); err != nil {
		t.Fatalf("activateSecureChannel() error = %v", err)
	}
	if listenCalls.Load() != 1 || client.transferConnectionCount() != 1 || client.conn[1] == nil {
		t.Fatalf("listener calls/data connections = %d/%d, conn=%v", listenCalls.Load(), client.transferConnectionCount(), client.conn[1])
	}
	offer := <-offerResult
	if offer.Type != message.TypeDERPOffer || offer.Message != tokenValue {
		t.Fatalf("offer = %+v", offer)
	}

	want := []byte("one DERP stream")
	go func() { _, _ = client.conn[1].Connection().Write(want) }()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(dataPeer, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("data connection traffic = %q, %v", got, err)
	}
	client.conn[1].Close()
}

func TestExperimentalDERPReceiverValidatesAndDialsOfferOnce(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	client := newExperimentalDERPTestClient(t, false, transport)
	dataLocal, dataPeer := net.Pipe()
	defer dataPeer.Close()
	tokenValue := experimentalDERPTestToken(t)
	var dialCalls atomic.Int32
	transport.dial = func(_ context.Context, gotToken string, _ derptransport.PathEvent) (net.Conn, error) {
		if gotToken != tokenValue {
			t.Errorf("dial token changed")
		}
		dialCalls.Add(1)
		return dataLocal, nil
	}
	attempt := newDERPAttempt(nil)
	if err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt); err != nil {
		t.Fatalf("process offer error = %v", err)
	}
	if dialCalls.Load() != 1 || client.conn[1] == nil {
		t.Fatalf("dial calls = %d, conn = %v", dialCalls.Load(), client.conn[1])
	}
	select {
	case <-attempt.derpSetupDone:
	default:
		t.Fatal("DERP setup was not marked complete")
	}
	if err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate offer error = %v", err)
	}
	client.conn[1].Close()
}

func TestExperimentalDERPOfferRejections(t *testing.T) {
	tokenValue := experimentalDERPTestToken(t)
	tests := []struct {
		name    string
		client  *Client
		offer   string
		wantErr string
	}{
		{name: "wrong role", client: newExperimentalDERPTestClient(t, true), offer: tokenValue, wantErr: "sender rejected"},
		{name: "not negotiated", client: &Client{Options: Options{}}, offer: tokenValue, wantErr: "without negotiated"},
		{name: "oversized", client: newExperimentalDERPTestClient(t, false), offer: strings.Repeat("x", derptransport.MaxTokenSize+1), wantErr: "invalid size"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: test.offer}, newDERPAttempt(nil))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("offer error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestExperimentalDERPSetupErrorHasNoFallbackOrTokenLeak(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	client := newExperimentalDERPTestClient(t, false, transport)
	tokenValue := experimentalDERPTestToken(t)
	transport.dial = func(context.Context, string, derptransport.PathEvent) (net.Conn, error) {
		return nil, errors.New("dial failed for " + tokenValue)
	}
	err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, newDERPAttempt(nil))
	if !errors.Is(err, ErrDERPConnection) {
		t.Fatalf("setup error = %v, want DERP classification", err)
	}
	if strings.Contains(err.Error(), tokenValue) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("setup error leaked token: %v", err)
	}
	if client.conn[1] != nil {
		t.Fatal("setup failure installed or fell back to a relay data connection")
	}
}

func TestExperimentalDERPSetupTimeoutCancelsDial(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	client := newExperimentalDERPTestClient(t, false, transport)
	tokenValue := experimentalDERPTestToken(t)
	canceled := make(chan struct{})
	transport.dial = func(ctx context.Context, _ string, _ derptransport.PathEvent) (net.Conn, error) {
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	attempt := newDERPAttempt(nil)
	attempt.derpSetupContext, attempt.derpSetupCancel = context.WithCancel(client.stop.ctx)
	attempt.derpSetupDeadline = time.Now().Add(20 * time.Millisecond)
	err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt)
	if !errors.Is(err, ErrDERPConnection) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("timed-out dial did not observe cancellation")
	}
}

func TestExperimentalDERPReconnectReplacesConnectionAndOffer(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	client := newExperimentalDERPTestClient(t, false, transport)
	firstToken := experimentalDERPTestToken(t)
	secondToken := experimentalDERPTestToken(t)
	if firstToken == secondToken {
		t.Fatal("test tokens are not fresh")
	}
	peers := make(chan net.Conn, 2)
	var dialCalls atomic.Int32
	transport.dial = func(context.Context, string, derptransport.PathEvent) (net.Conn, error) {
		local, peer := net.Pipe()
		peers <- peer
		dialCalls.Add(1)
		return local, nil
	}

	firstAttempt := newDERPAttempt(nil)
	if err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: firstToken}, firstAttempt); err != nil {
		t.Fatal(err)
	}
	firstPeer := <-peers
	client.closeAttempt()
	_ = firstPeer.Close()
	client.nextReconnectRoom = "fresh-reconnect-room"
	if err := client.resetForReconnectAttempt(1); err != nil {
		t.Fatal(err)
	}
	if client.peerDERP || client.derpOfferReceived {
		t.Fatal("reconnect retained DERP negotiation or offer state")
	}

	// A fresh PAKE on the reconnect advertises support again before a new offer.
	client.peerDERP = true
	secondAttempt := newDERPAttempt(nil)
	if err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: secondToken}, secondAttempt); err != nil {
		t.Fatal(err)
	}
	secondPeer := <-peers
	defer secondPeer.Close()
	if dialCalls.Load() != 2 || client.conn[1] == nil {
		t.Fatalf("reconnect dial calls/connection = %d/%v", dialCalls.Load(), client.conn[1])
	}
	client.conn[1].Close()
}

func TestUnencryptedExperimentalDERPOfferIsRejected(t *testing.T) {
	client := newExperimentalDERPTestClient(t, false)
	client.Key = nil
	payload, err := message.Encode(nil, message.Message{Type: message.TypeDERPOffer, Message: experimentalDERPTestToken(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.processMessage(payload, newDERPAttempt(nil))
	if err == nil || !strings.Contains(err.Error(), "unencrypted communication rejected") {
		t.Fatalf("unencrypted offer error = %v", err)
	}
}

func TestExperimentalDERPEndToEndFileTransfer(t *testing.T) {
	t.Run("legacy", func(t *testing.T) { testExperimentalDERPEndToEndFileTransfer(t, false) })
	t.Run("attach-group", func(t *testing.T) { testExperimentalDERPEndToEndFileTransfer(t, true) })
}

func testExperimentalDERPEndToEndFileTransfer(t *testing.T, grouped bool) {
	t.Helper()
	transport := newFakeDERPDataTransport(grouped)

	tokenValue := experimentalDERPTestToken(t)
	expectedStreams := 1
	if grouped {
		tokenValue = experimentalDERPGroupTestToken(t)
		expectedStreams = 4
	}
	var listenCalls, dialCalls atomic.Int32
	if grouped {
		listener := &pairedDERPGroupListener{
			tokenValue: tokenValue,
			bundles:    make(chan derptransport.Bundle, 1),
			closed:     make(chan struct{}),
		}
		transport.listenGroup = func(context.Context, derptransport.PathEvent) (derptransport.GroupListener, error) {
			listenCalls.Add(1)
			return listener, nil
		}
		transport.dialGroup = func(ctx context.Context, gotToken string, _ derptransport.PathEvent) (derptransport.Bundle, error) {
			if gotToken != tokenValue {
				return nil, errors.New("unexpected token")
			}
			dialCalls.Add(1)
			senderConnections := make([]net.Conn, expectedStreams)
			receiverConnections := make([]net.Conn, expectedStreams)
			for i := range expectedStreams {
				senderConnections[i], receiverConnections[i] = net.Pipe()
			}
			senderBundle := &fakeDERPBundle{connections: senderConnections, stats: derptransport.BundleStats{Mode: "raw-direct", Path: "direct", StreamCount: expectedStreams}}
			receiverBundle := &fakeDERPBundle{connections: receiverConnections, stats: derptransport.BundleStats{Mode: "raw-direct", Path: "direct", StreamCount: expectedStreams}}
			select {
			case listener.bundles <- senderBundle:
				return receiverBundle, nil
			case <-ctx.Done():
				_ = senderBundle.Close()
				_ = receiverBundle.Close()
				return nil, ctx.Err()
			}
		}
	} else {
		listener := &pairedDERPListener{
			tokenValue:  tokenValue,
			connections: make(chan net.Conn, 1),
			closed:      make(chan struct{}),
		}
		transport.listen = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
			listenCalls.Add(1)
			return listener, nil
		}
		transport.dial = func(ctx context.Context, gotToken string, _ derptransport.PathEvent) (net.Conn, error) {
			if gotToken != tokenValue {
				return nil, errors.New("unexpected token")
			}
			dialCalls.Add(1)
			senderConn, receiverConn := net.Pipe()
			select {
			case listener.connections <- senderConn:
				return receiverConn, nil
			case <-ctx.Done():
				_ = senderConn.Close()
				_ = receiverConn.Close()
				return nil, ctx.Err()
			}
		}
	}

	source, err := os.CreateTemp("", "croc-experimental-derp-")
	if err != nil {
		t.Fatal(err)
	}
	sourcePayload := bytes.Repeat([]byte("DERP data stream\n"), 4096)
	if _, err = source.Write(sourcePayload); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(source.Name())
	receivedName := filepath.Base(source.Name())
	defer os.Remove(receivedName)

	secret := "experimental-derp-file-transfer"
	sender, err := newClient(Options{
		IsSender:         true,
		SharedSecret:     secret,
		RelayAddress:     "127.0.0.1:8281",
		RelayPorts:       []string{"8281"},
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		Transport:        TransportAuto,
		DisableClipboard: true,
	}, transport)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newClient(Options{
		SharedSecret:     secret,
		RelayAddress:     "127.0.0.1:8281",
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		Transport:        TransportAuto,
		DisableClipboard: true,
	}, transport)
	if err != nil {
		t.Fatal(err)
	}
	files, folders, folderCount, err := GetFilesInfo([]string{source.Name()}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- sender.Send(files, folders, folderCount) }()
	time.Sleep(100 * time.Millisecond)
	go func() { errCh <- receiver.Receive() }()
	for range 2 {
		select {
		case transferErr := <-errCh:
			if transferErr != nil {
				t.Fatalf("experimental DERP transfer: %v", transferErr)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("experimental DERP transfer timed out")
		}
	}
	got, err := os.ReadFile(receivedName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sourcePayload) {
		t.Fatal("received payload differs")
	}
	if listenCalls.Load() != 1 || dialCalls.Load() != 1 {
		t.Fatalf("Attach setup calls = listen %d, dial %d", listenCalls.Load(), dialCalls.Load())
	}
	for _, client := range []*Client{sender, receiver} {
		for i := expectedStreams + 1; i < len(client.conn); i++ {
			if client.conn[i] != nil {
				t.Fatalf("croc relay data connection %d was opened in DERP mode", i)
			}
		}
	}
}

func TestAutoTransportFallsBackToRelayEndToEnd(t *testing.T) {
	transport := newFakeDERPDataTransport(false)
	var listenCalls, dialCalls atomic.Int32
	listener := &pairedDERPListener{
		tokenValue:  experimentalDERPTestToken(t),
		connections: make(chan net.Conn),
		closed:      make(chan struct{}),
	}
	transport.listen = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
		listenCalls.Add(1)
		return listener, nil
	}
	transport.dial = func(context.Context, string, derptransport.PathEvent) (net.Conn, error) {
		dialCalls.Add(1)
		return nil, errors.New("injected DERP dial failure")
	}

	source, err := os.CreateTemp("", "croc-auto-relay-")
	if err != nil {
		t.Fatal(err)
	}
	sourcePayload := bytes.Repeat([]byte("automatic relay fallback\n"), 2048)
	if _, err = source.Write(sourcePayload); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(source.Name())
	receivedName := filepath.Base(source.Name())
	defer os.Remove(receivedName)

	options := Options{
		SharedSecret:     "auto-derp-relay-fallback",
		RelayAddress:     "127.0.0.1:8281",
		RelayPorts:       []string{"8282", "8283", "8284", "8285"},
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		Transport:        TransportAuto,
		DisableClipboard: true,
	}
	senderOptions := options
	senderOptions.IsSender = true
	sender, err := newClient(senderOptions, transport)
	if err != nil {
		t.Fatal(err)
	}
	receiverOptions := options
	receiverOptions.RelayPorts = nil
	receiver, err := newClient(receiverOptions, transport)
	if err != nil {
		t.Fatal(err)
	}
	files, folders, folderCount, err := GetFilesInfo([]string{source.Name()}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- sender.Send(files, folders, folderCount) }()
	time.Sleep(100 * time.Millisecond)
	go func() { errCh <- receiver.Receive() }()
	for range 2 {
		select {
		case transferErr := <-errCh:
			if transferErr != nil {
				t.Fatalf("auto fallback transfer: %v", transferErr)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("auto fallback transfer timed out")
		}
	}
	got, err := os.ReadFile(receivedName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sourcePayload) {
		t.Fatal("received fallback payload differs")
	}
	if listenCalls.Load() != 1 || dialCalls.Load() != 1 {
		t.Fatalf("DERP setup calls = listen %d, dial %d", listenCalls.Load(), dialCalls.Load())
	}
	if !listener.closeOnce.Load() {
		t.Fatal("DERP listener was not closed during relay fallback")
	}
	for _, client := range []*Client{sender, receiver} {
		if client.selectedDataTransport.Load() != selectedTransportRelay {
			t.Fatalf("selected transport = %d, want relay", client.selectedDataTransport.Load())
		}
	}
}
