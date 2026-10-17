package croc

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/tcp"
)

type reservedTestListener struct {
	port   int
	closed atomic.Bool
}

func (l *reservedTestListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *reservedTestListener) Addr() net.Addr            { return &net.TCPAddr{Port: l.port} }
func (l *reservedTestListener) Close() error              { l.closed.Store(true); return nil }

func TestLocalRelayReservationSkipsBusyPortsAndHoldsSockets(t *testing.T) {
	var held []*reservedTestListener
	listen := func(_ context.Context, network, address string) (net.Listener, error) {
		if network != "tcp" || !strings.HasPrefix(address, ":") {
			t.Fatalf("reservation must use wildcard tcp sockets: %s %s", network, address)
		}
		for _, listener := range held {
			if listener.closed.Load() {
				t.Fatal("reservation closed a selected socket before handoff")
			}
		}
		port, _ := strconv.Atoi(strings.TrimPrefix(address, ":"))
		if port == 30000 || port == 30002 {
			return nil, &net.OpError{Op: "listen", Net: network, Err: syscall.EADDRINUSE}
		}
		listener := &reservedTestListener{port: port}
		held = append(held, listener)
		return listener, nil
	}
	listeners, err := reserveLocalRelayListeners(t.Context(), 30000, 3, listen)
	if err != nil {
		t.Fatal(err)
	}
	var ports []int
	for _, listener := range listeners {
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
		listener.Close()
	}
	if !slices.Equal(ports, []int{30001, 30003, 30004}) {
		t.Fatalf("reserved ports = %v", ports)
	}
}

func TestLocalRelayReservationFailureClosesPartialReservations(t *testing.T) {
	for _, cause := range []string{"busy range", "port range boundary", "cancellation"} {
		t.Run(cause, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var held []*reservedTestListener
			calls := 0
			listen := func(_ context.Context, _ string, address string) (net.Listener, error) {
				calls++
				if cause == "busy range" && calls > 1 {
					return nil, syscall.EADDRINUSE
				}
				_, port, err := net.SplitHostPort(address)
				if err != nil {
					t.Fatal(err)
				}
				portNumber, _ := strconv.Atoi(port)
				listener := &reservedTestListener{port: portNumber}
				held = append(held, listener)
				if cause == "cancellation" {
					cancel()
				}
				return listener, nil
			}
			firstPort := 30000
			if cause == "port range boundary" {
				firstPort = 65535
			}
			listeners, err := reserveLocalRelayListeners(ctx, firstPort, 2, listen)
			if err == nil || len(listeners) != 0 || len(held) == 0 {
				t.Fatalf("failed reservation = %v, %v; held = %d", listeners, err, len(held))
			}
			if cause == "cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			if cause == "busy range" && (calls != 200 || !errors.Is(err, syscall.EADDRINUSE)) {
				t.Fatalf("busy range: calls = %d, error = %v", calls, err)
			}
			if cause == "port range boundary" && calls != 1 {
				t.Fatalf("tried %d ports past tcp port boundary", calls)
			}
			for _, listener := range held {
				if !listener.closed.Load() {
					t.Fatal("failed reservation leaked a socket")
				}
			}
		})
	}
}

func TestLocalRelayStartupServesReservedPortsAndCancelsGroup(t *testing.T) {
	firstPort := freeConsecutiveTestPorts(t, 3)[0]
	busy, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", firstPort))
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	c := &Client{
		Options: Options{RelayPorts: []string{firstPort, "unused"}, RelayPassword: "pass123"},
		stop:    newStop(t.Context()),
	}
	defer c.stop.Cancel()
	if err := c.setupLocalRelay(); err != nil {
		t.Fatal(err)
	}
	ports := append([]string(nil), c.Options.RelayPorts...)
	if slices.Contains(ports, firstPort) {
		t.Fatal("local relay selected an existing loopback listener's port")
	}
	waitForRelayPorts(t, ports)
	controlAddress := net.JoinHostPort("127.0.0.1", c.localRelayPort)
	control, banner, _, err := tcp.ConnectToTCPServer(controlAddress, "pass123", "reserved-local-control")
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if banner != strings.Join(ports[1:], ",") {
		t.Fatalf("advertised ports = %q; reserved = %v", banner, ports)
	}
	for _, port := range ports {
		if err := tcp.PingServer(net.JoinHostPort("127.0.0.1", port)); err != nil {
			t.Fatalf("reserved port %s is not serving: %v", port, err)
		}
	}
	c.stop.Cancel()
	waitForLocalRelayPortsReleased(t, ports)
}

func TestLocalRelayStartupWorkerFailureClosesGroup(t *testing.T) {
	c := &Client{
		Options: Options{RelayPorts: []string{freeConsecutiveTestPorts(t, 2)[0], "unused"}},
		stop:    newStop(t.Context()),
	}
	defer c.stop.Cancel()
	started := make(chan net.Listener, 2)
	fail := make(chan struct{})
	cancelled := make(chan struct{})
	var calls atomic.Int32
	c.stop.run = func(ctx context.Context, _ string, listener net.Listener, _ string, _ ...string) error {
		first := calls.Add(1) == 1
		started <- listener
		if first {
			select {
			case <-fail:
				return errors.New("injected relay worker failure")
			case <-ctx.Done():
				return nil
			}
		}
		<-ctx.Done()
		close(cancelled)
		return nil
	}
	if err := c.setupLocalRelay(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("relay workers did not receive reserved listeners")
		}
	}
	close(fail)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("failed relay worker did not cancel its sibling")
	}
	waitForLocalRelayPortsReleased(t, c.Options.RelayPorts)
}

func TestLocalRelayStartupRejectsInvalidPorts(t *testing.T) {
	for _, ports := range [][]string{nil, {"invalid"}, {"0"}, {"65536"}} {
		c := &Client{Options: Options{RelayPorts: ports}, stop: newStop(t.Context())}
		if err := c.setupLocalRelay(); err == nil {
			t.Fatalf("invalid local ports accepted: %v", ports)
		}
		c.stop.Cancel()
	}
}

func waitForLocalRelayPortsReleased(t *testing.T, ports []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for _, port := range ports {
		for {
			listener, err := net.Listen("tcp", net.JoinHostPort("", port))
			if err == nil {
				listener.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("local relay leaked port %s: %v", port, err)
			}
			time.Sleep(time.Millisecond)
		}
	}
}
