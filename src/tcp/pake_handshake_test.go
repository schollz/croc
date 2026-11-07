package tcp

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/stretchr/testify/require"
)

// TestMalformedRelayPAKEDoesNotStopServer guards the pre-authentication relay
// handshake. A JSON object with no curve points used to panic inside
// pake.Update and terminate every listener in the process.
func TestMalformedRelayPAKEDoesNotStopServer(t *testing.T) {
	_, address, stopServer := startConfiguredTestServer(t)
	defer stopServer()

	for _, payload := range [][]byte{
		[]byte("{}"),
		[]byte("null"),
		[]byte(`{"Role":0}`),
		[]byte(`{"Role":0,"Xᵤ":1}`),
	} {
		client, err := comm.NewConnection(address, time.Second)
		require.NoError(t, err)
		require.NoError(t, client.Send(payload))
		require.NoError(t, client.Connection().SetReadDeadline(time.Now().Add(2*time.Second)))
		_, _ = io.Copy(io.Discard, client.Connection())
		client.Close()
		require.NoError(t, PingServer(address))
	}

	healthy, _, _, err := ConnectToTCPServer(address, "pass123", "still-up", time.Second)
	require.NoError(t, err)
	healthy.Close()
}

// TestMalformedRelayResponseDoesNotPanicClient guards the side that connects
// to a relay. The peer's first PAKE frame is untrusted and must not panic.
func TestMalformedRelayResponseDoesNotPanicClient(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte("null"),
		[]byte(`{"Role":1}`),
		[]byte(`{"Role":1,"Yᵤ":1}`),
		[]byte("{}"),
	} {
		t.Run(string(payload), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()

			accepted := make(chan struct{})
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer conn.Close()
				close(accepted)
				peer := comm.New(conn)
				if _, receiveErr := peer.Receive(); receiveErr != nil {
					return
				}
				_ = peer.Send(payload)
			}()

			_, _, _, err = ConnectToTCPServer(listener.Addr().String(), "pass123", "room", time.Second)
			require.Error(t, err)
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatalf("client timed out instead of rejecting the PAKE payload: %v", err)
			}
			select {
			case <-accepted:
			case <-time.After(2 * time.Second):
				t.Fatal("hostile relay did not accept the client")
			}
		})
	}
}
