package croc

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/tailcattransport"
)

func stagedTestBundle(path string) *tailcatDataBundle {
	local, remote := net.Pipe()
	return &tailcatDataBundle{
		connections: []net.Conn{local},
		stats: func() tailcattransport.BundleStats {
			return tailcattransport.BundleStats{Path: path, StreamCount: 1}
		},
		cleanup: func() error {
			_ = local.Close()
			return remote.Close()
		},
	}
}

func stagedTestClient(t *testing.T, accept func(context.Context) (*tailcatDataBundle, error)) (*Client, *fakeTailcatListener, *comm.Comm, *transferAttemptState) {
	t.Helper()
	listener := &fakeTailcatListener{offer: "staged-offer", accept: accept}
	provider := &fakeTailcatTransport{
		available: true,
		listen: func(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error) {
			return listener, nil
		},
	}
	local, remote := net.Pipe()
	control := comm.New(local)
	client := &Client{
		Options:                  Options{IsSender: true, Transport: TransportAuto, RelayPorts: []string{"1", "2", "3", "4"}},
		Key:                      []byte("0123456789abcdef0123456789abcdef"),
		peerInlineMetadata:       true,
		peerStagedTransport:      true,
		peerImplicitTailcatReady: true,
		stop:                     newStop(context.Background()),
		conn:                     []*comm.Comm{control},
		tailcat:                  tailcatClientState{transport: provider, peerCapable: true},
		stagedRelayDelayOverride: 5 * time.Millisecond,
		stagedSelectionOverride:  40 * time.Millisecond,
	}
	attempt := newTailcatTestAttempt(control)
	return client, listener, comm.New(remote), attempt
}

func readStagedMessages(t *testing.T, control *comm.Comm, key []byte, count int) <-chan []message.Message {
	t.Helper()
	result := make(chan []message.Message, 1)
	go func() {
		messages := make([]message.Message, 0, count)
		for range count {
			payload, err := control.Receive()
			if err != nil {
				result <- messages
				return
			}
			decoded, err := message.Decode(key, payload)
			if err != nil {
				result <- messages
				return
			}
			messages = append(messages, decoded)
		}
		result <- messages
	}()
	return result
}

func TestStagedTransportDirectTailcatWins(t *testing.T) {
	client, _, peer, attempt := stagedTestClient(t, func(context.Context) (*tailcatDataBundle, error) {
		return stagedTestBundle("direct"), nil
	})
	defer peer.Close()
	messages := readStagedMessages(t, peer, client.Key, 2)
	if err := client.activateStagedTransportSender(attempt); err != nil {
		t.Fatal(err)
	}
	defer client.closeTailcatBundle()
	got := <-messages
	if len(got) != 2 || got[0].Type != message.TypeTailcatOffer || got[1].Type != message.TypeTransportSelect || got[1].Message != string(TransportDERP) {
		t.Fatalf("messages = %+v", got)
	}
	if client.selectedDataTransport.Load() != selectedTransportTailcat {
		t.Fatal("Tailcat was not selected")
	}
}

func TestStagedTransportRelayStandbyWins(t *testing.T) {
	client, listener, peer, attempt := stagedTestClient(t, func(ctx context.Context) (*tailcatDataBundle, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer peer.Close()
	var opens atomic.Int32
	client.stagedRelayOpen = func(*transferAttemptState, []int, bool) error {
		opens.Add(1)
		return nil
	}
	messages := readStagedMessages(t, peer, client.Key, 4)
	if err := client.activateStagedTransportSender(attempt); err != nil {
		t.Fatal(err)
	}
	got := <-messages
	if len(got) != 4 || got[1].Type != message.TypeRelayStandby || got[2].Message != string(TransportRelay) || got[3].Type != message.TypeRelayRamp {
		t.Fatalf("messages = %+v", got)
	}
	if !listener.closed.Load() || client.selectedDataTransport.Load() != selectedTransportRelay {
		t.Fatal("relay selection did not clean up Tailcat")
	}
	deadline := time.Now().Add(time.Second)
	for opens.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if opens.Load() != 2 {
		t.Fatalf("relay open phases = %d", opens.Load())
	}
}

func TestStagedTransportUsesDERPWhenRelayFails(t *testing.T) {
	client, _, peer, attempt := stagedTestClient(t, func(context.Context) (*tailcatDataBundle, error) {
		return stagedTestBundle("derp"), nil
	})
	defer peer.Close()
	client.stagedRelayOpen = func(*transferAttemptState, []int, bool) error { return errors.New("relay failed") }
	messages := readStagedMessages(t, peer, client.Key, 3)
	if err := client.activateStagedTransportSender(attempt); err != nil {
		t.Fatal(err)
	}
	defer client.closeTailcatBundle()
	got := <-messages
	if len(got) != 3 || got[1].Type != message.TypeRelayStandby || got[2].Message != string(TransportDERP) {
		t.Fatalf("messages = %+v", got)
	}
}

func TestStagedTransportDeadlineCleansUp(t *testing.T) {
	client, listener, peer, attempt := stagedTestClient(t, func(ctx context.Context) (*tailcatDataBundle, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer peer.Close()
	client.stagedRelayOpen = func(*transferAttemptState, []int, bool) error { return errors.New("relay failed") }
	messages := readStagedMessages(t, peer, client.Key, 2)
	if err := client.activateStagedTransportSender(attempt); err == nil {
		t.Fatal("staged deadline unexpectedly succeeded")
	}
	<-messages
	if !listener.closed.Load() {
		t.Fatal("deadline did not close Tailcat listener")
	}
}
