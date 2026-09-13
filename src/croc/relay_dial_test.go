package croc

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
)

func TestRaceRelayTCPUsesFirstReachableAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	connection, address, err := raceRelayTCP(
		context.Background(),
		[]string{"127.0.0.1:1", listener.Addr().String()},
		time.Second,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if address != listener.Addr().String() {
		t.Fatalf("selected %q, want %q", address, listener.Addr())
	}
	select {
	case serverConnection := <-accepted:
		serverConnection.Close()
	case <-time.After(time.Second):
		t.Fatal("reachable address was not dialed")
	}
}

func TestRaceRelayTCPRejectsEmptyAddressList(t *testing.T) {
	if _, _, err := raceRelayTCP(context.Background(), nil, time.Second, 0); err == nil {
		t.Fatal("empty address list succeeded")
	}
}

func TestRaceRelayTCPClosesSimultaneousLoser(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	addresses := []string{"127.0.0.1:1", "127.0.0.1:2"}
	connections := make([]*comm.Comm, 2)
	peers := make([]net.Conn, 2)
	deadline, _ := ctx.Deadline()
	for i := range connections {
		local, peer := net.Pipe()
		connections[i] = comm.New(local)
		peers[i] = peer
		defer local.Close()
		defer peer.Close()
		if err := local.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		if err := peer.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	dial := func(_ context.Context, address string, _ time.Duration) (*comm.Comm, error) {
		started <- struct{}{}
		select {
		case <-release:
			if address == addresses[0] {
				return connections[0], nil
			}
			return connections[1], nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	result := make(chan rawRelayDialResult, 1)
	go func() {
		connection, address, err := raceRelayTCPWithDialer(ctx, addresses, time.Second, 0, dial)
		result <- rawRelayDialResult{connection: connection, address: address, err: err}
	}()
	for range connections {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("both dials did not start")
		}
	}
	close(release)
	var winner rawRelayDialResult
	select {
	case winner = <-result:
	case <-ctx.Done():
		t.Fatal("no winning connection returned")
	}
	if winner.err != nil {
		t.Fatal(winner.err)
	}
	winnerIndex := 0
	if winner.address == addresses[1] {
		winnerIndex = 1
	}
	if winner.address != addresses[winnerIndex] || winner.connection != connections[winnerIndex] {
		t.Fatal("returned connection does not match its address")
	}
	if _, err := peers[1-winnerIndex].Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("losing connection was not closed: %v", err)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := peers[winnerIndex].Write([]byte("x"))
		writeDone <- err
	}()
	var received [1]byte
	if _, err := io.ReadFull(winner.connection.Connection(), received[:]); err != nil {
		t.Fatalf("winning connection is not usable: %v", err)
	}
	if err := <-writeDone; err != nil || received[0] != 'x' {
		t.Fatalf("winning connection transfer: byte=%q, error=%v", received[0], err)
	}
}

func TestRaceRelayTCPCancelsPendingDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	local, peer := net.Pipe()
	defer local.Close()
	defer peer.Close()
	connection := comm.New(local)
	pending := make(chan struct{})
	canceled := make(chan struct{})
	dial := func(ctx context.Context, address string, _ time.Duration) (*comm.Comm, error) {
		if address == "127.0.0.1:1" {
			select {
			case <-pending:
				return connection, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		close(pending)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	winner, address, err := raceRelayTCPWithDialer(ctx, []string{"127.0.0.1:1", "127.0.0.1:2"}, time.Second, 0, dial)
	if err != nil || winner != connection || address != "127.0.0.1:1" {
		t.Fatalf("unexpected winner: address=%q, error=%v", address, err)
	}
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("losing dial was not canceled")
	}
}

func TestRelayControlCancellationClosesPendingHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{Options: Options{RelayPassword: "pass", RoomName: "room"}}
	result := make(chan error, 1)
	go func() {
		_, connectErr := client.connectRelayControlContext(ctx, listener.Addr().String())
		result <- connectErr
	}()
	var serverConnection net.Conn
	select {
	case serverConnection = <-accepted:
		defer serverConnection.Close()
	case <-time.After(time.Second):
		t.Fatal("relay handshake was not started")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled relay handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled relay handshake remained blocked")
	}
}
