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
		Capabilities:    token.CapabilityAttach,
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newExperimentalDERPTestClient(t *testing.T, sender bool) *Client {
	t.Helper()
	client, err := New(Options{
		SharedSecret:     "correct-horse-battery",
		IsSender:         sender,
		ExperimentalDERP: true,
		Curve:            "p256",
		RelayAddress:     "invalid relay address that must not be dialed",
		RelayPorts:       []string{"1", "2", "3", "4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.peerExperimentalDERP = true
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

func TestExperimentalDERPOptionIsNotRemembered(t *testing.T) {
	encoded, err := json.Marshal(Options{ExperimentalDERP: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "ExperimentalDERP") || strings.Contains(string(encoded), "experimental") {
		t.Fatalf("remembered options contain experimental DERP: %s", encoded)
	}
}

func TestExperimentalDERPNewRejectsUnsupportedModesAndRoute(t *testing.T) {
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
			test.options.ExperimentalDERP = true
			if _, err := New(test.options); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want %q", err, test.want)
			}
		})
	}
	t.Run("custom DERP route", func(t *testing.T) {
		t.Setenv(derpbind.CustomDERPServerEnv, "https://derp.example")
		_, err := New(Options{SharedSecret: "experimental-derp-route", ExperimentalDERP: true})
		if !errors.Is(err, ErrExperimentalDERPConnection) || !errors.Is(err, derptransport.ErrCustomRoute) {
			t.Fatalf("New() error = %v", err)
		}
	})
}

func TestExperimentalDERPRequiresBothPeers(t *testing.T) {
	for _, test := range []struct {
		name  string
		local bool
		peer  bool
	}{
		{name: "only local", local: true},
		{name: "only peer", peer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{
				Options:              Options{IsSender: true, ExperimentalDERP: test.local},
				peerExperimentalDERP: test.peer,
				stop:                 newStop(context.Background()),
			}
			if err := client.activateSecureChannel(newDERPAttempt(nil)); err == nil || !strings.Contains(err.Error(), "both peers") {
				t.Fatalf("mismatch error = %v", err)
			}
		})
	}
	if got := (&Client{Options: Options{ExperimentalDERP: true}}).pakeFeatures(); len(got) != 1 || got[0] != experimentalDERPFeature {
		t.Fatalf("PAKE features = %v", got)
	}
}

func TestExperimentalDERPSenderInstallsOneDataConnection(t *testing.T) {
	oldListen := listenExperimentalDERP
	defer func() { listenExperimentalDERP = oldListen }()

	client := newExperimentalDERPTestClient(t, true)
	controlLocal, controlPeer := net.Pipe()
	defer controlPeer.Close()
	client.conn[0] = comm.New(controlLocal)
	dataLocal, dataPeer := net.Pipe()
	defer dataPeer.Close()
	tokenValue := experimentalDERPTestToken(t)
	listener := &fakeDERPListener{tokenValue: tokenValue, conn: dataLocal}
	var listenCalls atomic.Int32
	listenExperimentalDERP = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
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
	oldDial := dialExperimentalDERP
	defer func() { dialExperimentalDERP = oldDial }()

	client := newExperimentalDERPTestClient(t, false)
	dataLocal, dataPeer := net.Pipe()
	defer dataPeer.Close()
	tokenValue := experimentalDERPTestToken(t)
	var dialCalls atomic.Int32
	dialExperimentalDERP = func(_ context.Context, gotToken string, _ derptransport.PathEvent) (net.Conn, error) {
		if gotToken != tokenValue {
			t.Errorf("dial token changed")
		}
		dialCalls.Add(1)
		return dataLocal, nil
	}
	attempt := newDERPAttempt(nil)
	if err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt); err != nil {
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
	if err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt); err == nil || !strings.Contains(err.Error(), "duplicate") {
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
			err := test.client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: test.offer}, newDERPAttempt(nil))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("offer error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestExperimentalDERPSetupErrorHasNoFallbackOrTokenLeak(t *testing.T) {
	oldDial := dialExperimentalDERP
	defer func() { dialExperimentalDERP = oldDial }()

	client := newExperimentalDERPTestClient(t, false)
	tokenValue := experimentalDERPTestToken(t)
	dialExperimentalDERP = func(context.Context, string, derptransport.PathEvent) (net.Conn, error) {
		return nil, errors.New("dial failed for " + tokenValue)
	}
	err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, newDERPAttempt(nil))
	if !errors.Is(err, ErrExperimentalDERPConnection) {
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
	oldDial := dialExperimentalDERP
	defer func() { dialExperimentalDERP = oldDial }()

	client := newExperimentalDERPTestClient(t, false)
	tokenValue := experimentalDERPTestToken(t)
	canceled := make(chan struct{})
	dialExperimentalDERP = func(ctx context.Context, _ string, _ derptransport.PathEvent) (net.Conn, error) {
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	attempt := newDERPAttempt(nil)
	attempt.derpSetupContext, attempt.derpSetupCancel = context.WithCancel(client.stop.ctx)
	attempt.derpSetupDeadline = time.Now().Add(20 * time.Millisecond)
	err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt)
	if !errors.Is(err, ErrExperimentalDERPConnection) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("timed-out dial did not observe cancellation")
	}
}

func TestExperimentalDERPReconnectReplacesConnectionAndOffer(t *testing.T) {
	oldDial := dialExperimentalDERP
	defer func() { dialExperimentalDERP = oldDial }()

	client := newExperimentalDERPTestClient(t, false)
	firstToken := experimentalDERPTestToken(t)
	secondToken := experimentalDERPTestToken(t)
	if firstToken == secondToken {
		t.Fatal("test tokens are not fresh")
	}
	peers := make(chan net.Conn, 2)
	var dialCalls atomic.Int32
	dialExperimentalDERP = func(context.Context, string, derptransport.PathEvent) (net.Conn, error) {
		local, peer := net.Pipe()
		peers <- peer
		dialCalls.Add(1)
		return local, nil
	}

	firstAttempt := newDERPAttempt(nil)
	if err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: firstToken}, firstAttempt); err != nil {
		t.Fatal(err)
	}
	firstPeer := <-peers
	client.closeAttempt()
	_ = firstPeer.Close()
	client.nextReconnectRoom = "fresh-reconnect-room"
	if err := client.resetForReconnectAttempt(1); err != nil {
		t.Fatal(err)
	}
	if client.peerExperimentalDERP || client.experimentalDERPOfferReceived {
		t.Fatal("reconnect retained DERP negotiation or offer state")
	}

	// A fresh PAKE on the reconnect advertises support again before a new offer.
	client.peerExperimentalDERP = true
	secondAttempt := newDERPAttempt(nil)
	if err := client.processExperimentalDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: secondToken}, secondAttempt); err != nil {
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
	oldListen, oldDial := listenExperimentalDERP, dialExperimentalDERP
	defer func() {
		listenExperimentalDERP = oldListen
		dialExperimentalDERP = oldDial
	}()

	tokenValue := experimentalDERPTestToken(t)
	listener := &pairedDERPListener{
		tokenValue:  tokenValue,
		connections: make(chan net.Conn, 1),
		closed:      make(chan struct{}),
	}
	var listenCalls, dialCalls atomic.Int32
	listenExperimentalDERP = func(context.Context, derptransport.PathEvent) (derptransport.Listener, error) {
		listenCalls.Add(1)
		return listener, nil
	}
	dialExperimentalDERP = func(ctx context.Context, gotToken string, _ derptransport.PathEvent) (net.Conn, error) {
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
	sender, err := New(Options{
		IsSender:         true,
		SharedSecret:     secret,
		RelayAddress:     "127.0.0.1:8281",
		RelayPorts:       []string{"8281"},
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		ExperimentalDERP: true,
		DisableClipboard: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := New(Options{
		SharedSecret:     secret,
		RelayAddress:     "127.0.0.1:8281",
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		ExperimentalDERP: true,
		DisableClipboard: true,
	})
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
		for i := 2; i < len(client.conn); i++ {
			if client.conn[i] != nil {
				t.Fatalf("croc relay data connection %d was opened in DERP mode", i)
			}
		}
	}
}
