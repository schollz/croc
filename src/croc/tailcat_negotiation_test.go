package croc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/tailcattransport"
)

type preparingTailcatTransport struct {
	*fakeTailcatTransport
	prepareCalls atomic.Int32
	prepare      func(context.Context) (any, error)
	listenValue  any
	prepared     any
	listener     tailcatDataListener
}

func (f *preparingTailcatTransport) Prepare(ctx context.Context) (any, error) {
	f.prepareCalls.Add(1)
	if f.prepare != nil {
		return f.prepare(ctx)
	}
	return f.prepared, nil
}

func (f *preparingTailcatTransport) ListenPrepared(_ context.Context, _ []byte, _ tailcattransport.PathEvent, prepared any) (tailcatDataListener, error) {
	f.listenValue = prepared
	return f.listener, nil
}

func TestTailcatPAKEFeaturesAndCompatibility(t *testing.T) {
	provider := &fakeTailcatTransport{available: true}
	receiverAuto := (&Client{Options: Options{Transport: TransportAuto}, tailcat: tailcatClientState{transport: provider}}).pakeFeatures()
	if !supportsFeature(receiverAuto, inlinePeerMetadataFeature) || !supportsFeature(receiverAuto, tailcatFeature) || supportsFeature(receiverAuto, tailcatRequiredFeature) {
		t.Fatalf("receiver auto features = %v", receiverAuto)
	}
	senderSmall := &Client{Options: Options{IsSender: true, Transport: TransportAuto}, tailcat: tailcatClientState{transport: provider}}
	if got := senderSmall.pakeFeatures(); !supportsFeature(got, inlinePeerMetadataFeature) || supportsFeature(got, tailcatFeature) {
		t.Fatalf("small sender auto features = %v", got)
	}
	senderLarge := &Client{Options: Options{IsSender: true, Transport: TransportAuto}, tailcat: tailcatClientState{transport: provider}}
	senderLarge.tailcat.transferBytes.Store(autoTailcatThresholdBytes)
	largeFeatures := senderLarge.pakeFeatures()
	if !supportsFeature(largeFeatures, tailcatFeature) || supportsFeature(largeFeatures, tailcatRequiredFeature) {
		t.Fatalf("large sender auto features = %v", largeFeatures)
	}
	strict := (&Client{Options: Options{IsSender: true, Transport: TransportDERP}, tailcat: tailcatClientState{transport: provider}}).pakeFeatures()
	if !supportsFeature(strict, tailcatFeature) || !supportsFeature(strict, tailcatRequiredFeature) {
		t.Fatalf("strict features = %v", strict)
	}
	for _, client := range []*Client{
		{Options: Options{Transport: TransportRelay}, tailcat: tailcatClientState{transport: provider}},
		{Options: Options{Transport: TransportAuto, OnlyLocal: true}, tailcat: tailcatClientState{transport: provider}},
		{Options: Options{Transport: TransportAuto}, tailcat: tailcatClientState{transport: &fakeTailcatTransport{}}},
	} {
		if got := client.pakeFeatures(); !supportsFeature(got, inlinePeerMetadataFeature) || supportsFeature(got, tailcatFeature) {
			t.Fatalf("unsupported mode advertised Tailcat: %v", got)
		}
	}
	if supportsFeature([]string{"unknown-legacy-transport-v1"}, tailcatFeature) {
		t.Fatal("an older peer was treated as Tailcat-capable")
	}
}

func TestTailcatPreparationIsReusedAfterSessionKeyExists(t *testing.T) {
	prepared := &struct{ region int }{region: 7}
	listener := &fakeTailcatListener{offer: "prepared"}
	provider := &preparingTailcatTransport{
		fakeTailcatTransport: &fakeTailcatTransport{available: true},
		prepared:             prepared,
		listener:             listener,
	}
	client := &Client{
		Options: Options{IsSender: true, Transport: TransportAuto},
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider},
		Key:     []byte("session key exists only after PAKE"),
	}
	client.tailcat.transferBytes.Store(128 * 1024 * 1024)
	client.startTailcatPreparation()
	got, err := client.listenTailcatData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != listener || provider.listenValue != prepared || provider.prepareCalls.Load() != 1 {
		t.Fatalf("prepared listener reuse failed: listener=%v value=%v calls=%d", got, provider.listenValue, provider.prepareCalls.Load())
	}
}

func TestInlinePeerMetadataSkipsDedicatedActivationExchange(t *testing.T) {
	fast := &Client{Options: Options{IsSender: true}, peerInlineMetadata: true}
	if err := fast.finishDataTransportActivation(); err != nil {
		t.Fatal(err)
	}
	if !fast.lifecycleSnapshot().ChannelSecured {
		t.Fatal("inline metadata did not complete secure-channel activation")
	}

	legacy := &Client{Options: Options{IsSender: true}}
	if err := legacy.finishDataTransportActivation(); err != nil {
		t.Fatal(err)
	}
	if legacy.lifecycleSnapshot().ChannelSecured {
		t.Fatal("legacy sender skipped the dedicated endpoint exchange")
	}
}

func TestTotalLogicalTransferSizeAndAutoThreshold(t *testing.T) {
	tests := []struct {
		name  string
		files []FileInfo
		want  int64
	}{
		{name: "below threshold", files: []FileInfo{{Size: autoTailcatThresholdBytes - 1}}, want: autoTailcatThresholdBytes - 1},
		{name: "at threshold", files: []FileInfo{{Size: autoTailcatThresholdBytes}}, want: autoTailcatThresholdBytes},
		{name: "above threshold", files: []FileInfo{{Size: autoTailcatThresholdBytes + 1}}, want: autoTailcatThresholdBytes + 1},
		{name: "combined size", files: []FileInfo{{Size: 200 * 1024 * 1024}, {Size: 110 * 1024 * 1024}}, want: 310 * 1024 * 1024},
		{name: "ignore non-positive", files: []FileInfo{{Size: -1}, {Size: 0}, {Size: 42}}, want: 42},
		{name: "saturate overflow", files: []FileInfo{{Size: math.MaxInt64 - 1}, {Size: 2}}, want: math.MaxInt64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := totalLogicalTransferSize(tt.files); got != tt.want {
				t.Fatalf("totalLogicalTransferSize() = %d, want %d", got, tt.want)
			}
		})
	}

	provider := &fakeTailcatTransport{available: true}
	for _, tt := range []struct {
		name     string
		bytes    int64
		eligible bool
	}{
		{name: "below", bytes: autoTailcatThresholdBytes - 1},
		{name: "equal", bytes: autoTailcatThresholdBytes, eligible: true},
		{name: "above", bytes: autoTailcatThresholdBytes + 1, eligible: true},
	} {
		t.Run("auto "+tt.name, func(t *testing.T) {
			client := &Client{Options: Options{IsSender: true, Transport: TransportAuto}, tailcat: tailcatClientState{transport: provider}}
			client.tailcat.transferBytes.Store(tt.bytes)
			if got := client.autoTailcatEligible(); got != tt.eligible {
				t.Fatalf("autoTailcatEligible() = %t, want %t", got, tt.eligible)
			}
		})
	}
}

func TestReceiverCannotSelectTransport(t *testing.T) {
	provider := &fakeTailcatTransport{available: true}
	if _, err := newClient(Options{SharedSecret: "receiver-auto", Curve: "p256", Transport: TransportAuto}, provider); err != nil {
		t.Fatalf("receiver auto transport rejected: %v", err)
	}
	for _, mode := range []TransportMode{TransportDERP, TransportRelay} {
		_, err := newClient(Options{SharedSecret: "receiver-explicit", Curve: "p256", Transport: mode}, provider)
		if err == nil || err.Error() != "transport selection is sender-only" {
			t.Fatalf("receiver transport %q error = %v", mode, err)
		}
	}
}

func TestResolveTransportModeForAvailability(t *testing.T) {
	tests := []struct {
		name       string
		mode       TransportMode
		available  bool
		want       TransportMode
		downgraded bool
	}{
		{name: "available auto", mode: TransportAuto, available: true, want: TransportAuto},
		{name: "unavailable auto", mode: TransportAuto, want: TransportAuto},
		{name: "available relay", mode: TransportRelay, available: true, want: TransportRelay},
		{name: "unavailable relay", mode: TransportRelay, want: TransportRelay},
		{name: "available derp", mode: TransportDERP, available: true, want: TransportDERP},
		{name: "unavailable derp", mode: TransportDERP, want: TransportRelay, downgraded: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, downgraded := resolveTransportMode(test.mode, test.available)
			if got != test.want || downgraded != test.downgraded {
				t.Fatalf("resolveTransportMode(%q, %t) = (%q, %t); want (%q, %t)", test.mode, test.available, got, downgraded, test.want, test.downgraded)
			}
		})
	}
}

func TestUnavailableTailcatUsesRelayInAutoAndDowngradesStrict(t *testing.T) {
	provider := &unavailableTestDataTransport{}
	auto, err := newClient(Options{
		SharedSecret: "tailcat-unavailable-auto",
		Transport:    TransportAuto,
		Curve:        "p256",
	}, provider)
	if err != nil {
		t.Fatalf("auto transport rejected: %v", err)
	}
	if features := auto.pakeFeatures(); !supportsFeature(features, inlinePeerMetadataFeature) || supportsFeature(features, tailcatFeature) {
		t.Fatalf("unavailable auto transport advertised Tailcat: %v", features)
	}
	strict, err := newClient(Options{
		SharedSecret: "tailcat-unavailable-strict",
		IsSender:     true,
		Transport:    TransportDERP,
		Curve:        "p256",
		ShowQrCode:   true,
	}, provider)
	if err != nil {
		t.Fatalf("strict unavailable transport rejected: %v", err)
	}
	if strict.Options.Transport != TransportRelay {
		t.Fatalf("strict unavailable transport = %q; want relay", strict.Options.Transport)
	}
	if features := strict.pakeFeatures(); supportsFeature(features, tailcatFeature) || supportsFeature(features, tailcatRequiredFeature) {
		t.Fatalf("downgraded transport advertised Tailcat: %v", features)
	}
}

func TestStrictTailcatTransportRequiresCapablePeer(t *testing.T) {
	c := &Client{
		Options: Options{IsSender: true, Transport: TransportDERP},
		tailcat: tailcatClientState{
			transport: &fakeTailcatTransport{available: true},
		},
		stop: newStop(context.Background()),
	}
	err := c.activateSecureChannel(&transferAttemptState{})
	if err == nil || !strings.Contains(err.Error(), "Tailcat-capable peer") {
		t.Fatalf("error = %v", err)
	}
}

func TestTailcatOfferMustBeEncrypted(t *testing.T) {
	payload, err := message.Encode(nil, message.Message{Type: message.TypeTailcatOffer, Message: "tc-offer"})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{}
	done, err := c.processMessage(payload, &transferAttemptState{})
	if !done || err == nil || !strings.Contains(err.Error(), "unencrypted communication rejected") {
		t.Fatalf("done=%v error=%v", done, err)
	}
}

func TestInstallTailcatBundleInstallsEveryStreamAndCleansOnce(t *testing.T) {
	const streams = 3
	connections := make([]net.Conn, streams)
	peers := make([]net.Conn, streams)
	for i := range connections {
		connections[i], peers[i] = net.Pipe()
		defer peers[i].Close()
	}
	var cleanupCalls atomic.Int32
	bundle := &tailcatDataBundle{
		connections: connections,
		stats: func() tailcattransport.BundleStats {
			return tailcattransport.BundleStats{Path: "direct", StreamCount: streams, SetupDuration: time.Millisecond}
		},
		cleanup: func() error {
			cleanupCalls.Add(1)
			for _, conn := range connections {
				_ = conn.Close()
			}
			return nil
		},
	}
	c := &Client{Options: Options{IsSender: true}, conn: make([]*comm.Comm, streams+1)}
	attempt := &transferAttemptState{tailcat: tailcatAttemptState{setupDone: make(chan struct{})}}
	if err := c.installTailcatBundle(bundle, nil, attempt); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= streams; i++ {
		if c.connection(i) == nil {
			t.Fatalf("stream %d was not installed", i-1)
		}
	}
	c.closeTailcatBundle()
	c.closeTailcatBundle()
	if cleanupCalls.Load() != 1 {
		t.Fatalf("cleanup called %d times", cleanupCalls.Load())
	}
}

func TestNegotiatedTailcatSenderSelectsAndInstallsEveryStream(t *testing.T) {
	const streams = 3
	key := bytes.Repeat([]byte{0x42}, 32)
	senderBundle, receiverBundle := newPipeDataBundles(streams)
	defer receiverBundle.Close()
	listener := &fakeTailcatListener{
		offer: "authenticated-tailcat-offer",
		accept: func(context.Context) (*tailcatDataBundle, error) {
			return senderBundle, nil
		},
	}
	provider := &fakeTailcatTransport{available: true}
	provider.listen = func(_ context.Context, gotKey []byte, _ tailcattransport.PathEvent) (tailcatDataListener, error) {
		if !bytes.Equal(gotKey, key) {
			t.Fatal("listener did not receive the confirmed PAKE key")
		}
		return listener, nil
	}
	provider.validate = func(offer string, gotKey []byte) error {
		if offer != listener.offer || !bytes.Equal(gotKey, key) {
			return errors.New("offer was not bound to the confirmed PAKE key")
		}
		return nil
	}

	controlLocal, controlPeer := net.Pipe()
	defer controlPeer.Close()
	client := &Client{
		Options: Options{IsSender: true, Transport: TransportAuto},
		Key:     key,
		conn:    make([]*comm.Comm, streams+1),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider, peerCapable: true},
	}
	client.tailcat.transferBytes.Store(autoTailcatThresholdBytes)
	client.setConnection(0, comm.New(controlLocal))
	attempt := newTailcatTestAttempt(client.connection(0))
	peerResult := make(chan error, 1)
	go func() {
		peer := comm.New(controlPeer)
		payload, err := peer.Receive()
		if err != nil {
			peerResult <- err
			return
		}
		offer, err := message.Decode(key, payload)
		if err != nil || offer.Type != message.TypeTailcatOffer || offer.Message != listener.offer {
			peerResult <- fmt.Errorf("offer = %+v: %w", offer, err)
			return
		}
		if err := message.Send(peer, key, message.Message{Type: message.TypeTailcatStatus, Message: tailcatStatusReady}); err != nil {
			peerResult <- err
			return
		}
		payload, err = peer.Receive()
		if err != nil {
			peerResult <- err
			return
		}
		selection, err := message.Decode(key, payload)
		if err != nil || selection.Type != message.TypeTransportSelect || selection.Message != string(TransportDERP) {
			peerResult <- fmt.Errorf("selection = %+v: %w", selection, err)
			return
		}
		peerResult <- nil
	}()

	if err := client.activateSecureChannel(attempt); err != nil {
		t.Fatal(err)
	}
	if err := <-peerResult; err != nil {
		t.Fatal(err)
	}
	if client.selectedDataTransport.Load() != selectedTransportTailcat {
		t.Fatalf("selected transport = %d", client.selectedDataTransport.Load())
	}
	for i := 1; i <= streams; i++ {
		if client.connection(i) == nil {
			t.Fatalf("Tailcat stream %d was not installed", i-1)
		}
	}
	client.closeTailcatBundle()
	if !listener.closed.Load() {
		t.Fatal("Tailcat listener was not closed with its installed bundle")
	}
}

func TestTailcatReceiverRequestsCoordinatedRelayFallback(t *testing.T) {
	key := bytes.Repeat([]byte{0x24}, 32)
	provider := &fakeTailcatTransport{available: true}
	provider.dial = func(context.Context, string, []byte, tailcattransport.PathEvent) (*tailcatDataBundle, error) {
		return nil, errors.New("injected Tailcat dial failure")
	}
	controlLocal, controlPeer := net.Pipe()
	defer controlPeer.Close()
	client := &Client{
		Options: Options{Transport: TransportAuto},
		Key:     key,
		conn:    make([]*comm.Comm, 2),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider, peerCapable: true},
	}
	client.setConnection(0, comm.New(controlLocal))
	attempt := newTailcatTestAttempt(client.connection(0))
	statusResult := make(chan message.Message, 1)
	go func() {
		payload, err := comm.New(controlPeer).Receive()
		if err != nil {
			statusResult <- message.Message{Message: err.Error()}
			return
		}
		status, decodeErr := message.Decode(key, payload)
		if decodeErr != nil {
			statusResult <- message.Message{Message: decodeErr.Error()}
			return
		}
		statusResult <- status
	}()
	if err := client.processTailcatOffer(message.Message{Type: message.TypeTailcatOffer, Message: "offer"}, attempt); err != nil {
		t.Fatal(err)
	}
	status := <-statusResult
	if status.Type != message.TypeTailcatStatus || status.Message != tailcatStatusFallback {
		t.Fatalf("fallback status = %+v", status)
	}
	if client.selectedDataTransport.Load() != selectedTransportUnset {
		t.Fatal("failed Tailcat bundle was installed before relay selection")
	}
}

func TestStrictTailcatFailureDoesNotFallback(t *testing.T) {
	provider := &fakeTailcatTransport{available: true}
	provider.dial = func(context.Context, string, []byte, tailcattransport.PathEvent) (*tailcatDataBundle, error) {
		return nil, errors.New("injected Tailcat dial failure")
	}
	client := &Client{
		Options: Options{Transport: TransportAuto},
		Key:     bytes.Repeat([]byte{0x12}, 32),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider, peerCapable: true, peerRequired: true},
	}
	err := client.processTailcatOffer(message.Message{Type: message.TypeTailcatOffer, Message: "offer"}, newTailcatTestAttempt(nil))
	if !errors.Is(err, ErrDERPConnection) {
		t.Fatalf("strict Tailcat error = %v", err)
	}
	if client.selectedDataTransport.Load() != selectedTransportUnset {
		t.Fatal("strict Tailcat failure selected a fallback transport")
	}
}

func TestAutoTransportSkipsTailcatForOlderPeer(t *testing.T) {
	var listenCalls atomic.Int32
	provider := &fakeTailcatTransport{available: true}
	provider.listen = func(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error) {
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
		conn:    make([]*comm.Comm, 2),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider},
	}
	if err := client.activateSecureChannel(newTailcatTestAttempt(nil)); !errors.Is(err, ErrRelayConnection) {
		t.Fatalf("older-peer relay error = %v", err)
	}
	if listenCalls.Load() != 0 {
		t.Fatalf("Tailcat listener called %d times for an older peer", listenCalls.Load())
	}
}

func TestAutoTransportSkipsTailcatBelowThreshold(t *testing.T) {
	var listenCalls atomic.Int32
	provider := &fakeTailcatTransport{available: true}
	provider.listen = func(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error) {
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
		conn:    make([]*comm.Comm, 2),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider, peerCapable: true},
	}
	client.tailcat.transferBytes.Store(autoTailcatThresholdBytes - 1)
	if err := client.activateSecureChannel(newTailcatTestAttempt(nil)); !errors.Is(err, ErrRelayConnection) {
		t.Fatalf("small-transfer relay error = %v", err)
	}
	if listenCalls.Load() != 0 {
		t.Fatalf("Tailcat listener called %d times below the auto threshold", listenCalls.Load())
	}
}

func TestTailcatSetupDeadlineCancelsDial(t *testing.T) {
	provider := &fakeTailcatTransport{available: true}
	canceled := make(chan struct{})
	provider.dial = func(ctx context.Context, _ string, _ []byte, _ tailcattransport.PathEvent) (*tailcatDataBundle, error) {
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	client := &Client{
		Options: Options{Transport: TransportAuto},
		Key:     bytes.Repeat([]byte{0x64}, 32),
		stop:    newStop(context.Background()),
		tailcat: tailcatClientState{transport: provider, peerCapable: true, peerRequired: true},
	}
	attempt := newTailcatTestAttempt(nil)
	attempt.tailcat.setupContext, attempt.tailcat.setupCancel = context.WithCancel(client.stop.ctx)
	attempt.tailcat.setupDeadline = time.Now().Add(20 * time.Millisecond)
	err := client.processTailcatOffer(message.Message{Type: message.TypeTailcatOffer, Message: "offer"}, attempt)
	if !errors.Is(err, ErrDERPConnection) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("timed-out Tailcat dial did not observe cancellation")
	}
}
