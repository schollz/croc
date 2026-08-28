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
