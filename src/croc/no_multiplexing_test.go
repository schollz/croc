package croc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/utils"
)

func TestNoMultiplexingMatrix(t *testing.T) {
	cases := []struct {
		name        string
		senderNoMux bool
		recvNoMux   bool
		wantDataCon int
	}{
		{name: "both-multiplex", senderNoMux: false, recvNoMux: false, wantDataCon: 2},
		{name: "receiver-no-mux", senderNoMux: false, recvNoMux: true, wantDataCon: 1},
		{name: "sender-no-mux", senderNoMux: true, recvNoMux: false, wantDataCon: 1},
		{name: "both-no-mux", senderNoMux: true, recvNoMux: true, wantDataCon: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			relay := startTestRelay(t, 2)

			root := t.TempDir()
			srcDir := filepath.Join(root, "src")
			dstDir := filepath.Join(root, "dst")
			if err := os.MkdirAll(srcDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dstDir, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dstDir)

			payload := bytes.Repeat([]byte("croc-no-multiplexing-256k-chunk\n"), 8192)
			srcPath := filepath.Join(srcDir, "payload.bin")
			if err := os.WriteFile(srcPath, payload, 0o600); err != nil {
				t.Fatal(err)
			}

			secret := fmt.Sprintf("no-mux-%s-%d", tc.name, time.Now().UnixNano())
			sender, err := newTestClient(Options{
				IsSender:         true,
				SharedSecret:     secret,
				RelayAddress:     relay.address,
				RelayPorts:       append([]string(nil), relay.allPorts...),
				RelayPassword:    "pass123",
				NoPrompt:         true,
				DisableLocal:     true,
				DisableClipboard: true,
				Curve:            "p256",
				Overwrite:        true,
				NoCompress:       true,
				NoMultiplexing:   tc.senderNoMux,
			})
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := newTestClient(Options{
				IsSender:         false,
				SharedSecret:     secret,
				RelayAddress:     relay.address,
				RelayPassword:    "pass123",
				NoPrompt:         true,
				DisableLocal:     true,
				DisableClipboard: true,
				Curve:            "p256",
				Overwrite:        true,
				NoCompress:       true,
				NoMultiplexing:   tc.recvNoMux,
			})
			if err != nil {
				t.Fatal(err)
			}

			files, folders, folderCount, err := GetFilesInfo([]string{srcPath}, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}

			results := make(chan error, 2)
			go func() { results <- sender.Send(files, folders, folderCount) }()
			go func() { results <- receiver.Receive() }()
			waitForTransferResults(t, results, 2, 10*time.Second, sender, receiver)

			got, err := os.ReadFile(filepath.Join(dstDir, "payload.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("received payload mismatch: got %d bytes, want %d", len(got), len(payload))
			}

			if gotConns := countDataConnections(sender); gotConns != tc.wantDataCon {
				t.Fatalf("sender data connections = %d, want %d", gotConns, tc.wantDataCon)
			}
			if gotConns := countDataConnections(receiver); gotConns != tc.wantDataCon {
				t.Fatalf("receiver data connections = %d, want %d", gotConns, tc.wantDataCon)
			}
		})
	}
}

func countDataConnections(c *Client) int {
	count := 0
	for _, conn := range c.connectionsSnapshot()[1:] {
		if conn != nil {
			count++
		}
	}
	return count
}

func TestNoMultiplexingLegacySenderFallbackAndHandshake(t *testing.T) {
	t.Run("receiver falls back when legacy sender omits feature", func(t *testing.T) {
		receiver, err := newTestClient(Options{
			IsSender:         false,
			SharedSecret:     "legacy-sender-fallback",
			RelayPorts:       []string{"9001", "9002", "9003"},
			NoPrompt:         true,
			DisableLocal:     true,
			DisableClipboard: true,
			Curve:            "p256",
			NoMultiplexing:   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !supportsFeature(receiver.pakeFeatures(), noMultiplexingFeature) {
			t.Fatal("receiver with NoMultiplexing did not advertise no-multiplexing-v1")
		}

		local, remote := net.Pipe()
		defer local.Close()
		defer remote.Close()
		receiver.setConnection(0, comm.New(local))
		receiver.pakeInitiator = append([]byte(nil), receiver.Pake.Bytes()...)
		receiver.pakeCurve = receiver.Options.Curve

		senderPake, err := pakekey.Init([]byte(receiver.pakePassphrase), 1, receiver.Options.Curve, pakekey.PurposeTransfer, receiver.Options.RoomName)
		if err != nil {
			t.Fatal(err)
		}
		if err := senderPake.Update(receiver.pakeInitiator); err != nil {
			t.Fatal(err)
		}

		confirmDone := make(chan message.Message, 1)
		go func() {
			raw, err := comm.New(remote).Receive()
			if err != nil {
				confirmDone <- message.Message{}
				return
			}
			msg, _ := message.Decode(nil, raw)
			confirmDone <- msg
		}()

		err = receiver.processMessagePake(message.Message{
			Type:     message.TypePAKE,
			Version:  pakekey.ProtocolVersion,
			Bytes:    senderPake.Bytes(),
			Bytes2:   make([]byte, pakekey.SaltSize),
			Features: []string{inlinePeerMetadataFeature},
		}, newTailcatTestAttempt(nil))
		if err != nil {
			t.Fatal(err)
		}
		confirm := <-confirmDone
		if confirm.Type != message.TypePAKEConfirm {
			t.Fatalf("expected TypePAKEConfirm, got %v", confirm.Type)
		}
		if receiver.peerNoMultiplexing {
			t.Fatal("receiver marked peerNoMultiplexing without sender confirmation")
		}
		if receiver.useSingleRelayChannel() {
			t.Fatal("receiver restricted to single relay channel against legacy sender")
		}
		if got := receiver.relayDataPortCount(); got != 3 {
			t.Fatalf("receiver.relayDataPortCount() = %d, want 3", got)
		}
	})

	t.Run("sender echoes no-multiplexing-v1 when requested by receiver", func(t *testing.T) {
		sender, err := newTestClient(Options{
			IsSender:         true,
			SharedSecret:     "sender-echo-no-mux",
			RelayPorts:       []string{"9001", "9002", "9003"},
			NoPrompt:         true,
			DisableLocal:     true,
			DisableClipboard: true,
			Curve:            "p256",
			NoMultiplexing:   false,
		})
		if err != nil {
			t.Fatal(err)
		}

		local, remote := net.Pipe()
		defer local.Close()
		defer remote.Close()
		sender.setConnection(0, comm.New(local))

		recvPake, err := pakekey.Init([]byte(sender.pakePassphrase), 0, "p256", pakekey.PurposeTransfer, sender.Options.RoomName)
		if err != nil {
			t.Fatal(err)
		}

		replyDone := make(chan message.Message, 1)
		go func() {
			raw, err := comm.New(remote).Receive()
			if err != nil {
				replyDone <- message.Message{}
				return
			}
			msg, _ := message.Decode(nil, raw)
			replyDone <- msg
		}()

		err = sender.processMessagePake(message.Message{
			Type:     message.TypePAKE,
			Version:  pakekey.ProtocolVersion,
			Bytes:    recvPake.Bytes(),
			Bytes2:   []byte("p256"),
			Features: []string{inlinePeerMetadataFeature, noMultiplexingFeature},
		}, newTailcatTestAttempt(nil))
		if err != nil {
			t.Fatal(err)
		}
		reply := <-replyDone
		if reply.Type != message.TypePAKE {
			t.Fatalf("expected TypePAKE, got %v", reply.Type)
		}
		if !supportsFeature(reply.Features, noMultiplexingFeature) {
			t.Fatalf("sender PAKE reply omitted %s: %v", noMultiplexingFeature, reply.Features)
		}
		if !sender.peerNoMultiplexing || !sender.useSingleRelayChannel() || sender.relayDataPortCount() != 1 {
			t.Fatalf("unexpected sender state: peerNoMux=%t single=%t ports=%d", sender.peerNoMultiplexing, sender.useSingleRelayChannel(), sender.relayDataPortCount())
		}
	})

	t.Run("receiver completes end-to-end against legacy multiplexing sender over multi-port relay", func(t *testing.T) {
		relay := startTestRelay(t, 2)

		root := t.TempDir()
		srcDir := filepath.Join(root, "src")
		dstDir := filepath.Join(root, "dst")
		if err := os.MkdirAll(srcDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dstDir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dstDir)

		chunkSize := models.TCP_BUFFER_SIZE / 2
		payload := make([]byte, 2*chunkSize)
		for i := range payload {
			payload[i] = byte((i*31 + 7) % 251)
		}
		srcPath := filepath.Join(srcDir, "legacy.bin")
		if err := os.WriteFile(srcPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		fileHash, err := utils.HashFile(srcPath, defaultHashAlgorithm)
		if err != nil {
			t.Fatal(err)
		}

		secret := fmt.Sprintf("legacy-e2e-%d", time.Now().UnixNano())
		receiver, err := newTestClient(Options{
			IsSender:         false,
			SharedSecret:     secret,
			RelayAddress:     relay.address,
			RelayPassword:    "pass123",
			NoPrompt:         true,
			DisableLocal:     true,
			DisableClipboard: true,
			Curve:            "p256",
			Overwrite:        true,
			NoMultiplexing:   true,
		})
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		senderErr := make(chan error, 1)
		go func() {
			senderErr <- runLegacyMultiplexingSender(ctx, relay, receiver.Options.RoomName, receiver.pakePassphrase, "legacy.bin", payload, fileHash)
		}()

		recvErr := make(chan error, 1)
		go func() {
			recvErr <- receiver.Receive()
		}()

		for range 2 {
			select {
			case err := <-senderErr:
				senderErr = nil
				if err != nil {
					cancelClients(receiver)
					t.Fatalf("legacy sender failed: %v", err)
				}
			case err := <-recvErr:
				recvErr = nil
				if err != nil {
					cancel()
					t.Fatalf("receiver failed: %v", err)
				}
			case <-ctx.Done():
				cancelClients(receiver)
				t.Fatalf("transfer timed out: %v", ctx.Err())
			}
		}

		got, err := os.ReadFile(filepath.Join(dstDir, "legacy.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("received payload mismatch: got %d bytes, want %d", len(got), len(payload))
		}
		if gotConns := countDataConnections(receiver); gotConns != 2 {
			t.Fatalf("receiver data connections = %d, want 2", gotConns)
		}
	})
}

func runLegacyMultiplexingSender(ctx context.Context, relay testRelay, room, passphrase, fileName string, payload, fileHash []byte) error {
	ctrlConn, _, _, _, err := tcp.ConnectToTCPServerControlContext(ctx, relay.address, "pass123", room)
	if err != nil {
		return err
	}
	defer ctrlConn.Close()

	for {
		frame, err := ctrlConn.Receive()
		if err != nil {
			return err
		}
		if bytes.Equal(frame, handshakeRequest) {
			break
		}
	}

	rawPake, err := ctrlConn.Receive()
	if err != nil {
		return err
	}
	pakeMsg, err := message.Decode(nil, rawPake)
	if err != nil {
		return err
	}
	if pakeMsg.Type != message.TypePAKE {
		return fmt.Errorf("expected TypePAKE, got %v", pakeMsg.Type)
	}
	curve := string(pakeMsg.Bytes2)
	pakeState, err := pakekey.Init([]byte(passphrase), 1, curve, pakekey.PurposeTransfer, room)
	if err != nil {
		return err
	}
	if err := pakeState.Update(pakeMsg.Bytes); err != nil {
		return err
	}
	sharedKey, err := pakeState.SessionKey()
	if err != nil {
		return err
	}
	salt := make([]byte, pakekey.SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	responder := pakeState.Bytes()
	keys, err := pakekey.Derive(sharedKey, pakekey.Context{
		Purpose:   pakekey.PurposeTransfer,
		Room:      room,
		Curve:     curve,
		Initiator: pakeMsg.Bytes,
		Responder: responder,
		Salt:      salt,
	})
	if err != nil {
		return err
	}
	if err := message.Send(ctrlConn, nil, message.Message{
		Type:    message.TypePAKE,
		Version: pakekey.ProtocolVersion,
		Bytes:   responder,
		Bytes2:  salt,
	}); err != nil {
		return err
	}

	rawConfirm, err := ctrlConn.Receive()
	if err != nil {
		return err
	}
	confirmMsg, err := message.Decode(nil, rawConfirm)
	if err != nil {
		return err
	}
	if confirmMsg.Type != message.TypePAKEConfirm || !pakekey.Confirm(keys.ConfirmationA, confirmMsg.Bytes) {
		return fmt.Errorf("invalid recipient PAKE confirmation")
	}
	if err := message.Send(ctrlConn, nil, message.Message{
		Type:    message.TypePAKEConfirm,
		Version: pakekey.ProtocolVersion,
		Bytes:   keys.ConfirmationB,
	}); err != nil {
		return err
	}

	dataAEAD, err := crypt.NewAESGCM(keys.EncryptionKey)
	if err != nil {
		return err
	}
	dataConns := make([]*comm.Comm, len(relay.dataPorts))
	for i, port := range relay.dataPorts {
		dc, _, _, _, err := tcp.ConnectToTCPServerWithCapabilityContext(
			ctx,
			net.JoinHostPort("127.0.0.1", port),
			"pass123",
			fmt.Sprintf("%s-%d", room, i),
			"",
		)
		if err != nil {
			return err
		}
		defer dc.Close()
		dataConns[i] = dc
	}

	chunkSize := len(payload) / len(dataConns)
	for {
		raw, err := ctrlConn.Receive()
		if err != nil {
			return err
		}
		msg, err := message.Decode(keys.EncryptionKey, raw)
		if err != nil {
			return err
		}
		switch msg.Type {
		case message.TypeExternalIP:
			if err := message.Send(ctrlConn, keys.EncryptionKey, message.Message{
				Type:    message.TypeExternalIP,
				Message: "127.0.0.1",
			}); err != nil {
				return err
			}
			infoBytes, err := json.Marshal(SenderInfo{
				FilesToTransfer: []FileInfo{{
					Name:         fileName,
					FolderRemote: ".",
					Size:         int64(len(payload)),
					Hash:         fileHash,
					ModTime:      time.Now(),
					Mode:         0o600,
				}},
				NoCompress:       true,
				HashAlgorithm:    defaultHashAlgorithm,
				ReconnectVersion: ReconnectVersion,
			})
			if err != nil {
				return err
			}
			if err := message.Send(ctrlConn, keys.EncryptionKey, message.Message{
				Type:  message.TypeFileInfo,
				Bytes: infoBytes,
			}); err != nil {
				return err
			}
		case message.TypeRecipientReady:
			for i, dc := range dataConns {
				offset := i * chunkSize
				chunk := payload[offset : offset+chunkSize]
				frame := make([]byte, 8+len(chunk))
				binary.LittleEndian.PutUint64(frame[:8], uint64(offset))
				copy(frame[8:], chunk)
				encrypted, err := crypt.EncryptAEADTo(nil, frame, dataAEAD)
				if err != nil {
					return err
				}
				if err := dc.Send(encrypted); err != nil {
					return err
				}
			}
		case message.TypeCloseSender:
			if err := message.Send(ctrlConn, keys.EncryptionKey, message.Message{
				Type: message.TypeCloseRecipient,
			}); err != nil {
				return err
			}
		case message.TypeFinished:
			return nil
		}
	}
}

func TestNoMultiplexingStagedTransportAndReconnectReset(t *testing.T) {
	t.Run("staged sender opens only port 0 when single channel negotiated", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			noMux       bool
			peerNoMux   bool
			wantIndices []int
		}{
			{name: "sender NoMultiplexing", noMux: true, peerNoMux: false, wantIndices: []int{0}},
			{name: "peer NoMultiplexing", noMux: false, peerNoMux: true, wantIndices: []int{0}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				client, _, peer, attempt := stagedTestClient(t, func(ctx context.Context) (*tailcatDataBundle, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				})
				defer client.stop.Cancel()
				defer peer.Close()
				client.Options.NoMultiplexing = tc.noMux
				client.peerNoMultiplexing = tc.peerNoMux

				var mu sync.Mutex
				var calls [][]int
				client.relayDataOpen = func(_ *transferAttemptState, indices []int, _ bool) error {
					mu.Lock()
					calls = append(calls, append([]int(nil), indices...))
					mu.Unlock()
					return nil
				}

				messages := readStagedMessages(t, peer, client.Key, 4)
				if err := client.activateStagedTransportSender(attempt); err != nil {
					t.Fatal(err)
				}
				if got := <-messages; len(got) != 4 {
					t.Fatalf("staged messages = %d, want 4", len(got))
				}

				mu.Lock()
				defer mu.Unlock()
				if len(calls) != 1 || !slices.Equal(calls[0], tc.wantIndices) {
					t.Fatalf("relayDataOpen calls = %v, want [%v]", calls, tc.wantIndices)
				}
			})
		}
	})

	t.Run("staged receiver standby and ramp respect relayDataPortCount", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			noMux       bool
			peerNoMux   bool
			wantStandby []int
			wantRamp    []int
		}{
			{name: "confirmed no-mux", noMux: true, peerNoMux: true, wantStandby: []int{0}, wantRamp: nil},
			{name: "legacy sender fallback", noMux: true, peerNoMux: false, wantStandby: []int{0, 1}, wantRamp: []int{2, 3}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				client := &Client{
					Options:             Options{Transport: TransportAuto, RelayPorts: []string{"1", "2", "3", "4"}, NoMultiplexing: tc.noMux},
					conn:                make([]*comm.Comm, 1),
					peerInlineMetadata:  true,
					peerStagedTransport: true,
					peerNoMultiplexing:  tc.peerNoMux,
					stop:                newStop(t.Context()),
					tailcat:             tailcatClientState{transport: &fakeTailcatTransport{available: true}, peerCapable: true},
				}
				attempt := newTailcatTestAttempt(nil)
				var mu sync.Mutex
				var standbyIndices []int
				rampDone := make(chan []int, 1)
				callNum := 0
				client.relayDataOpen = func(_ *transferAttemptState, indices []int, startReceive bool) error {
					mu.Lock()
					callNum++
					n := callNum
					mu.Unlock()
					if n == 1 {
						standbyIndices = append([]int(nil), indices...)
						return nil
					}
					rampDone <- append([]int(nil), indices...)
					return nil
				}

				if err := client.processRelayStandby(attempt); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(standbyIndices, tc.wantStandby) {
					t.Fatalf("standby indices = %v, want %v", standbyIndices, tc.wantStandby)
				}
				client.selectedDataTransport.Store(selectedTransportRelay)
				if err := client.processRelayRamp(attempt); err != nil {
					t.Fatal(err)
				}
				if tc.wantRamp == nil {
					if indices := relayRampIndices(client.relayDataPortCount()); indices != nil {
						t.Fatalf("relayRampIndices = %v, want nil", indices)
					}
					select {
					case extra := <-rampDone:
						t.Fatalf("unexpected relay ramp open: %v", extra)
					default:
					}
				} else {
					select {
					case gotRamp := <-rampDone:
						if !slices.Equal(gotRamp, tc.wantRamp) {
							t.Fatalf("ramp indices = %v, want %v", gotRamp, tc.wantRamp)
						}
					case <-time.After(time.Second):
						t.Fatal("timed out waiting for relay ramp")
					}
				}
			})
		}
	})

	t.Run("resetForReconnectAttempt clears peerNoMultiplexing", func(t *testing.T) {
		client := &Client{
			Options:            Options{IsSender: false, Curve: "p256"},
			nextReconnectRoom:  "reconnect-room-1",
			pakePassphrase:     "secret",
			peerNoMultiplexing: true,
		}
		if err := client.resetForReconnectAttempt(1); err != nil {
			t.Fatal(err)
		}
		if client.peerNoMultiplexing {
			t.Fatal("resetForReconnectAttempt did not clear peerNoMultiplexing")
		}
	})
}
