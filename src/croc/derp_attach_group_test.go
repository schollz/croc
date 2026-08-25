package croc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/derptransport"
	"github.com/schollz/croc/v11/src/message"
)

type pairedDERPGroupListener struct {
	tokenValue string
	bundles    chan derptransport.Bundle
	closed     chan struct{}
	closeOnce  atomic.Bool
}

func (l *pairedDERPGroupListener) Token() string { return l.tokenValue }
func (l *pairedDERPGroupListener) Accept(ctx context.Context) (derptransport.Bundle, error) {
	select {
	case bundle := <-l.bundles:
		return bundle, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (l *pairedDERPGroupListener) Close() error {
	if l.closeOnce.CompareAndSwap(false, true) {
		close(l.closed)
	}
	return nil
}

type fakeDERPBundle struct {
	connections []net.Conn
	stats       derptransport.BundleStats
	closed      atomic.Bool
	closeOnce   sync.Once
}

func (b *fakeDERPBundle) Connections() []net.Conn {
	return append([]net.Conn(nil), b.connections...)
}
func (b *fakeDERPBundle) Stats() derptransport.BundleStats { return b.stats }
func (b *fakeDERPBundle) Close() error {
	var closeErr error
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		for _, conn := range b.connections {
			if err := conn.Close(); closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

type fakeDERPGroupListener struct {
	tokenValue string
	bundle     derptransport.Bundle
	err        error
	closed     atomic.Bool
}

func (l *fakeDERPGroupListener) Token() string { return l.tokenValue }
func (l *fakeDERPGroupListener) Accept(context.Context) (derptransport.Bundle, error) {
	return l.bundle, l.err
}
func (l *fakeDERPGroupListener) Close() error {
	l.closed.Store(true)
	return nil
}

func TestExperimentalDERPSenderInstallsNegotiatedAttachGroup(t *testing.T) {
	transport := newFakeDERPDataTransport(true)
	client := newExperimentalDERPTestClient(t, true, transport)
	client.peerDERPAttachGroup = true
	controlLocal, controlPeer := net.Pipe()
	defer controlPeer.Close()
	client.conn[0] = comm.New(controlLocal)

	const streamCount = 4
	localConnections := make([]net.Conn, streamCount)
	peerConnections := make([]net.Conn, streamCount)
	for i := range streamCount {
		localConnections[i], peerConnections[i] = net.Pipe()
		defer peerConnections[i].Close()
	}
	bundle := &fakeDERPBundle{
		connections: localConnections,
		stats: derptransport.BundleStats{
			Mode:        "raw-direct",
			Path:        "direct",
			StreamCount: streamCount,
		},
	}
	listener := &fakeDERPGroupListener{
		tokenValue: experimentalDERPGroupTestToken(t),
		bundle:     bundle,
	}
	var listenCalls atomic.Int32
	transport.listenGroup = func(context.Context, derptransport.PathEvent) (derptransport.GroupListener, error) {
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
	if listenCalls.Load() != 1 || client.transferConnectionCount() != streamCount {
		t.Fatalf("listener calls/data connections = %d/%d", listenCalls.Load(), client.transferConnectionCount())
	}
	for i := range streamCount {
		if client.conn[i+1] == nil {
			t.Fatalf("data connection %d was not installed", i)
		}
		want := []byte{byte(i), 0x5a}
		go func(conn net.Conn) { _, _ = conn.Write(want) }(client.conn[i+1].Connection())
		got := make([]byte, len(want))
		if _, err := io.ReadFull(peerConnections[i], got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("data connection %d traffic = %q, %v", i, got, err)
		}
	}
	if offer := <-offerResult; offer.Type != message.TypeDERPOffer || offer.Message != listener.tokenValue {
		t.Fatalf("offer = %+v", offer)
	}

	client.conn[1].Close()
	want := []byte("still-open")
	go func() { _, _ = client.conn[2].Connection().Write(want) }()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(peerConnections[1], got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("sibling connection traffic = %q, %v", got, err)
	}
	if bundle.closed.Load() {
		t.Fatal("closing one stream closed the AttachGroup owner")
	}
	client.closeDERPBundle()
	client.closeDERPBundle()
	if !bundle.closed.Load() || !listener.closed.Load() {
		t.Fatal("idempotent bundle cleanup did not close the group and listener")
	}
}

func TestExperimentalDERPReceiverDialsNegotiatedAttachGroup(t *testing.T) {
	transport := newFakeDERPDataTransport(true)
	client := newExperimentalDERPTestClient(t, false, transport)
	client.peerDERPAttachGroup = true
	tokenValue := experimentalDERPGroupTestToken(t)
	localConnections := make([]net.Conn, 3)
	peerConnections := make([]net.Conn, 3)
	for i := range localConnections {
		localConnections[i], peerConnections[i] = net.Pipe()
		defer peerConnections[i].Close()
	}
	bundle := &fakeDERPBundle{
		connections: localConnections,
		stats: derptransport.BundleStats{
			Mode:        "raw-direct",
			Path:        "direct",
			StreamCount: len(localConnections),
		},
	}
	var dialCalls atomic.Int32
	transport.dialGroup = func(_ context.Context, gotToken string, _ derptransport.PathEvent) (derptransport.Bundle, error) {
		if gotToken != tokenValue {
			t.Errorf("dial token changed")
		}
		dialCalls.Add(1)
		return bundle, nil
	}

	attempt := newDERPAttempt(nil)
	if err := client.processDERPOffer(message.Message{Type: message.TypeDERPOffer, Message: tokenValue}, attempt); err != nil {
		t.Fatalf("process offer error = %v", err)
	}
	if dialCalls.Load() != 1 || client.transferConnectionCount() != len(localConnections) {
		t.Fatalf("dial calls/data connections = %d/%d", dialCalls.Load(), client.transferConnectionCount())
	}
	for i := range localConnections {
		if client.conn[i+1] == nil {
			t.Fatalf("data connection %d was not installed", i)
		}
	}
	client.closeDERPBundle()
}

func TestDERPSenderLaneSnapshotSurvivesTeardown(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "croc-derp-lanes-")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{FilesToTransfer: []FileInfo{{Size: 0}}}
	client.selectedDataTransport.Store(selectedTransportDERP)
	client.derpTransferConnections.Store(8)
	connectionCount := client.transferConnectionCount()
	client.derpTransferConnections.Store(0)
	attempt := newDERPAttempt(nil)

	for lane := range connectionCount - 1 {
		client.sendData(lane, connectionCount, nil, file, attempt)
		if _, err := file.Stat(); err != nil {
			t.Fatalf("lane %d closed the shared file early: %v", lane, err)
		}
	}
	client.sendData(connectionCount-1, connectionCount, nil, file, attempt)
	if _, err := file.Stat(); !os.IsNotExist(err) && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("final lane left the shared file open: %v", err)
	}
}
