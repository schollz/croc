package croc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/tailcattransport"
)

func TestTailcatDataEOFBeforeAndAfterTerminalState(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal=%t", terminal), func(t *testing.T) {
			local, peer := net.Pipe()
			client := &Client{stop: newStop(context.Background())}
			client.selectedDataTransport.Store(selectedTransportTailcat)
			client.tailcat.terminal.Store(terminal)
			attempt := newTailcatTestAttempt(nil)
			done := make(chan struct{})
			go func() {
				client.receiveData(0, comm.New(local), attempt)
				close(done)
			}()
			_ = peer.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Tailcat receive loop did not exit on EOF")
			}
			select {
			case got := <-attempt.errc:
				if terminal {
					t.Fatalf("terminal EOF was reported as an error: %v", got)
				}
				if !errors.Is(got, ErrDERPConnection) {
					t.Fatalf("premature EOF error = %v", got)
				}
			default:
				if !terminal {
					t.Fatal("premature EOF was not reported")
				}
			}
		})
	}
}

func TestTailcatMultiStreamEndToEndFileTransfer(t *testing.T) {
	const streams = 3
	listener := &fakeTailcatListener{offer: "paired-tailcat-offer"}
	bundles := make(chan *tailcatDataBundle, 1)
	listener.accept = func(ctx context.Context) (*tailcatDataBundle, error) {
		select {
		case bundle := <-bundles:
			return bundle, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	provider := &fakeTailcatTransport{available: true}
	provider.listen = func(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error) {
		return listener, nil
	}
	provider.dial = func(ctx context.Context, offer string, _ []byte, _ tailcattransport.PathEvent) (*tailcatDataBundle, error) {
		if offer != listener.offer {
			return nil, errors.New("Tailcat offer changed in transit")
		}
		sender, receiver := newPipeDataBundles(streams)
		select {
		case bundles <- sender:
			return receiver, nil
		case <-ctx.Done():
			_ = sender.Close()
			_ = receiver.Close()
			return nil, ctx.Err()
		}
	}
	provider.validate = func(offer string, _ []byte) error {
		if offer != listener.offer {
			return tailcattransport.ErrInvalidOffer
		}
		return nil
	}

	testRoot := t.TempDir()
	sourceDir := filepath.Join(testRoot, "source")
	receiveDir := filepath.Join(testRoot, "receive")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(receiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "tailcat-framing.bin")
	sourcePayload := bytes.Repeat([]byte("Tailcat application framing\n"), 4096)
	if err := os.WriteFile(sourcePath, sourcePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(receiveDir)

	options := Options{
		SharedSecret:     "tailcat-full-file-transfer",
		RelayAddress:     "127.0.0.1:8281",
		RelayPorts:       []string{"8282", "8283", "8284", "8285"},
		RelayPassword:    "pass123",
		NoPrompt:         true,
		DisableLocal:     true,
		Curve:            "p256",
		Overwrite:        true,
		Transport:        TransportAuto,
		DisableClipboard: true,
	}
	senderOptions := options
	senderOptions.IsSender = true
	senderOptions.Transport = TransportDERP
	sender, err := newClient(senderOptions, provider)
	if err != nil {
		t.Fatal(err)
	}
	receiverOptions := options
	receiverOptions.RelayPorts = nil
	receiver, err := newClient(receiverOptions, provider)
	if err != nil {
		t.Fatal(err)
	}
	files, folders, folderCount, err := GetFilesInfo([]string{sourcePath}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- sender.Send(files, folders, folderCount) }()
	time.Sleep(100 * time.Millisecond)
	go func() { errCh <- receiver.Receive() }()
	for range 2 {
		select {
		case transferErr := <-errCh:
			if transferErr != nil {
				t.Fatalf("Tailcat transfer: %v", transferErr)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("Tailcat transfer timed out")
		}
	}
	received, err := os.ReadFile(filepath.Join(receiveDir, filepath.Base(sourcePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, sourcePayload) {
		t.Fatal("Tailcat transfer changed the file payload")
	}
	for _, client := range []*Client{sender, receiver} {
		if client.selectedDataTransport.Load() != selectedTransportTailcat {
			t.Fatalf("selected transport = %d, want Tailcat", client.selectedDataTransport.Load())
		}
	}
}

func TestDefaultTailcatStreamConfigIsValid(t *testing.T) {
	config := tailcatBuildConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if config.StreamCount != 8 {
		t.Fatalf("production Tailcat stream count = %d", config.StreamCount)
	}
}
