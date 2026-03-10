package croc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v10/src/pool"
	"github.com/schollz/croc/v10/src/tcp"
	"github.com/schollz/croc/v10/src/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freeLocalPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	return port
}

func waitForPoolReady(t *testing.T, baseURL string) {
	t.Helper()
	client := pool.NewClient(baseURL)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.FetchRelays(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pool server did not become ready at %s", baseURL)
}

func TestFederatedPoolEndToEnd(t *testing.T) {
	poolPort := freeLocalPort(t)
	relayPortA := freeLocalPort(t)
	relayPortB := freeLocalPort(t)

	poolListen := net.JoinHostPort("127.0.0.1", poolPort)
	poolURL := fmt.Sprintf("http://%s", poolListen)

	poolServer := pool.NewServer(poolListen)
	poolServer.HeartbeatTimeout = 30 * time.Second
	poolServer.CleanupInterval = 60 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = poolServer.Start(ctx)
	}()
	waitForPoolReady(t, poolURL)

	go func() {
		_ = tcp.Run("debug", "127.0.0.1", relayPortB, "pass123")
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		_ = tcp.Run("debug", "127.0.0.1", relayPortA, "pass123", relayPortB)
	}()
	time.Sleep(200 * time.Millisecond)

	poolClient := pool.NewClient(poolURL)
	registerCtx, registerCancel := context.WithTimeout(context.Background(), 2*time.Second)
	registerResp, err := poolClient.Register(registerCtx, pool.RegisterRequest{
		IPv4:     "127.0.0.1",
		Ports:    []string{relayPortA, relayPortB},
		Password: "pass123",
	})
	registerCancel()
	require.NoError(t, err)
	require.NotEmpty(t, registerResp.RelayID)

	relayCode := utils.GetRandomName(registerResp.RelayID)
	parsedRelayID, parsedSecret := pool.ParseTransferCode(relayCode)
	require.Equal(t, registerResp.RelayID, parsedRelayID)
	require.NotEmpty(t, parsedSecret)

	sharedTempDir := t.TempDir()
	sendFile := filepath.Join(sharedTempDir, "federated-pool-send.txt")
	receivedFile := "federated-pool-send.txt"
	defer os.Remove(receivedFile)
	require.NoError(t, os.WriteFile(sendFile, []byte("hello from federated pool test"), 0o644))

	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  relayCode,
		Debug:         true,
		RelayAddress:  net.JoinHostPort("127.0.0.1", relayPortA),
		RelayPorts:    []string{relayPortA},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	require.NoError(t, err)

	receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 2*time.Second)
	relays, err := poolClient.FetchRelays(receiveCtx)
	receiveCancel()
	require.NoError(t, err)
	require.NotEmpty(t, relays)

	selectedRelay, found := pool.Relay{}, false
	for _, r := range relays {
		if r.RelayID == parsedRelayID {
			selectedRelay = r
			found = true
			break
		}
	}
	require.True(t, found)

	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  relayCode,
		Debug:         true,
		RelayAddress:  net.JoinHostPort(selectedRelay.IPv4, selectedRelay.Ports[0]),
		RelayPassword: selectedRelay.Password,
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	require.NoError(t, err)

	filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{sendFile}, false, false, []string{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)

	go func() {
		defer wg.Done()
		errCh <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	}()
	time.Sleep(150 * time.Millisecond)
	go func() {
		defer wg.Done()
		errCh <- receiver.Receive()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for federated transfer")
	}
	close(errCh)
	for transferErr := range errCh {
		require.NoError(t, transferErr)
	}

	b, err := os.ReadFile(receivedFile)
	require.NoError(t, err)
	assert.Equal(t, "hello from federated pool test", string(b))
}
