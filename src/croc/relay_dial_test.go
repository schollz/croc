package croc

import (
	"context"
	"net"
	"testing"
	"time"
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
	listeners := make([]net.Listener, 2)
	accepted := make([]chan net.Conn, 2)
	addresses := make([]string, 2)
	for i := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		listeners[i] = listener
		addresses[i] = listener.Addr().String()
		accepted[i] = make(chan net.Conn, 1)
		go func(index int) {
			connection, acceptErr := listeners[index].Accept()
			if acceptErr == nil {
				accepted[index] <- connection
			}
		}(i)
	}

	winner, winnerAddress, err := raceRelayTCP(context.Background(), addresses, time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer winner.Close()
	winnerIndex := 0
	if winnerAddress == addresses[1] {
		winnerIndex = 1
	}
	serverConnections := make([]net.Conn, 2)
	for i := range accepted {
		select {
		case serverConnections[i] = <-accepted[i]:
			defer serverConnections[i].Close()
		case <-time.After(time.Second):
			t.Fatalf("address %d was not connected", i)
		}
	}
	loserIndex := 1 - winnerIndex
	loser := serverConnections[loserIndex]
	if err := loser.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := loser.Read(make([]byte, 1)); err == nil {
		t.Fatal("losing connection remained open")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("losing connection was not closed before the deadline")
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
