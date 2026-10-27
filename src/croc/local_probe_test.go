package croc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/stretchr/testify/require"
)

// A probe reply must be consumed before the connection switches from local
// discovery to compressed transfer messages, even if the discovery budget has
// expired. Cover both replies and reads that stall inside a framed message.
func TestReceiveFallsBackToRelayAfterDelayedLocalProbe(t *testing.T) {
	for _, test := range []struct {
		name       string
		delayIPs   bool
		frameBytes int
	}{
		{name: "pake-response"},
		{name: "partial-header", frameBytes: 4},
		{name: "partial-body", frameBytes: 16},
		{name: "ip-response", delayIPs: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			relay := startTestRelay(t, 1)
			address, delayed := startDelayedLocalProbeProxy(t, relay.address, test.delayIPs, test.frameBytes)
			receiveDir := t.TempDir()
			t.Chdir(receiveDir)
			payload := []byte("a delayed local discovery reply must not enter the transfer decoder\n")
			source := filepath.Join(t.TempDir(), "probe.txt")
			require.NoError(t, os.WriteFile(source, payload, 0o600))
			files, folders, folderCount, err := GetFilesInfo([]string{source}, false, false, nil)
			require.NoError(t, err)

			options := Options{
				IsSender: true, SharedSecret: "probe-delayed-response", RelayAddress: address,
				RelayPassword: "pass123", NoPrompt: true, DisableLocal: true,
				DisableClipboard: true, Curve: "p256", Transport: TransportRelay,
			}
			sender, err := newTestClient(options)
			require.NoError(t, err)
			options.IsSender, options.Transport = false, TransportAuto
			// Force the public-relay probe without running multicast discovery.
			options.TestFlag = true
			receiver, err := newTestClient(options)
			require.NoError(t, err)
			defer cancelClients(sender, receiver)

			results := make(chan error, 2)
			go func() { results <- sender.Send(files, folders, folderCount) }()
			go func() { results <- receiver.Receive() }()
			waitForTransferResults(t, results, 2, 10*time.Second, sender, receiver)
			select {
			case <-delayed:
			default:
				t.Fatal("transfer did not exercise a delayed local-probe reply")
			}
			received, err := os.ReadFile(filepath.Join(receiveDir, "probe.txt"))
			require.NoError(t, err)
			require.Equal(t, payload, received)
		})
	}
}

// Delay only the selected local-probe reply. Relay authentication, keepalives,
// and the subsequent transfer frames pass through unchanged.
func startDelayedLocalProbeProxy(t *testing.T, relayAddress string, delayIPs bool, frameBytes int) (string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	delayed := make(chan struct{}, 1)
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer client.Close()
				upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", relayAddress)
				if err != nil {
					return
				}
				defer upstream.Close()
				stopClose := context.AfterFunc(ctx, func() {
					client.Close()
					upstream.Close()
				})
				defer stopClose()
				workers.Go(func() {
					defer upstream.Close()
					defer client.Close()
					_, _ = io.Copy(upstream, client)
				})

				reader := comm.New(upstream)
				waitingForIPs, didDelay := false, false
				for {
					data, err := reader.Receive()
					if err != nil {
						return
					}
					var probe SimpleMessage
					_ = json.Unmarshal(data, &probe)
					pakeReply := probe.Kind == "pake2"
					shouldDelay := !didDelay && ((pakeReply && !delayIPs) || (waitingForIPs && !bytes.Equal(data, []byte{1})))
					if pakeReply && delayIPs {
						waitingForIPs = true
					}
					frame := make([]byte, 8+len(data))
					copy(frame, comm.MAGIC_BYTES)
					binary.LittleEndian.PutUint32(frame[4:8], uint32(len(data)))
					copy(frame[8:], data)
					if shouldDelay {
						didDelay = true
						if _, err := client.Write(frame[:frameBytes]); err != nil {
							return
						}
						timer := time.NewTimer(localProbeResponseTimeout + 250*time.Millisecond)
						select {
						case <-timer.C:
						case <-ctx.Done():
							timer.Stop()
							return
						}
						delayed <- struct{}{}
						frame = frame[frameBytes:]
					}
					if _, err := client.Write(frame); err != nil {
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		cancel()
		listener.Close()
		done := make(chan struct{})
		go func() {
			workers.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("local-probe proxy did not stop")
		}
	})
	return listener.Addr().String(), delayed
}
