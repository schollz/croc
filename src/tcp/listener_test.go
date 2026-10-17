package tcp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type trackedListener struct {
	net.Listener
	closed    atomic.Bool
	acceptErr error
}

func (l *trackedListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

func (l *trackedListener) Accept() (net.Conn, error) {
	if l.acceptErr != nil {
		return nil, l.acceptErr
	}
	return l.Listener.Accept()
}

func newTrackedListener(t *testing.T) *trackedListener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return &trackedListener{Listener: l}
}

func TestRunWithListenerServesReservedSocket(t *testing.T) {
	listener := newTrackedListener(t)
	address := listener.Addr().String()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithListener(listener, "pass123", WithCtx(ctx), WithLogLevel("error"), WithBanner("reserved-ports"))
	}()
	waitForRelayPort(t, address)
	if listener.closed.Load() {
		t.Fatal("reserved listener was closed before serving")
	}
	client, banner, _, err := ConnectToTCPServer(address, "pass123", "reserved-room")
	if err != nil {
		t.Fatalf("handshake on reserved listener: %v", err)
	}
	defer client.Close()
	if banner != "reserved-ports" {
		t.Fatalf("banner = %q", banner)
	}
	pending := openPartialHandshake(t, address)
	defer pending.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancel relay: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop after cancellation")
	}
	if !listener.closed.Load() {
		t.Fatal("relay did not close its owned listener")
	}
	waitForRawConnectionClose(t, pending)
	waitForConnectionClose(t, client)
	reused, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listener address was not reusable: %v", err)
	}
	reused.Close()
}

func TestRunWithListenerClosesOnStartupFailure(t *testing.T) {
	t.Run("configuration", func(t *testing.T) {
		listener := newTrackedListener(t)
		if err := RunWithListener(listener, "pass", WithMaxPendingHandshakes(0)); err == nil {
			t.Fatal("invalid configuration was accepted")
		}
		if !listener.closed.Load() {
			t.Fatal("configuration failure leaked listener")
		}
	})
	t.Run("cancelled before startup", func(t *testing.T) {
		listener := newTrackedListener(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		done := make(chan error, 1)
		go func() { done <- RunWithListener(listener, "pass", WithCtx(ctx), WithLogLevel("error")) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("relay blocked with an already-cancelled context")
		}
		if !listener.closed.Load() {
			t.Fatal("cancelled startup leaked listener")
		}
	})
	t.Run("accept", func(t *testing.T) {
		listener := newTrackedListener(t)
		want := errors.New("injected accept failure")
		listener.acceptErr = want
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- RunWithListener(listener, "pass", WithCtx(ctx), WithLogLevel("error")) }()
		select {
		case err := <-done:
			if !errors.Is(err, want) {
				t.Fatalf("accept error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("relay did not cancel workers after accept failure")
		}
		if !listener.closed.Load() {
			t.Fatal("accept failure leaked listener")
		}
	})
}
