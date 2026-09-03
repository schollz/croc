package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type testBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *testBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *testBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func testSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

type testServerEvents struct {
	input   chan string
	resize  chan WindowSize
	initial chan WindowSize
	user    chan string
}

func serveTestSSH(connection net.Conn, signer gossh.Signer, events testServerEvents) error {
	config := &gossh.ServerConfig{
		PasswordCallback: func(_ gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if !bytes.Equal(password, []byte("QkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkI")) {
				return nil, errors.New("permission denied")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	server, channels, requests, err := gossh.NewServerConn(connection, config)
	if err != nil {
		return err
	}
	defer server.Close()
	events.user <- server.User()
	go gossh.DiscardRequests(requests)
	request := <-channels
	if request == nil || request.ChannelType() != "session" {
		return errors.New("expected SSH session channel")
	}
	channel, channelRequests, err := request.Accept()
	if err != nil {
		return err
	}
	defer channel.Close()
	go func() {
		input := make([]byte, len("browser-input"))
		if _, readErr := io.ReadFull(channel, input); readErr == nil {
			events.input <- string(input)
		}
	}()
	for request := range channelRequests {
		switch request.Type {
		case "pty-req":
			var payload struct {
				Terminal                     string
				Columns, Rows, Width, Height uint32
				Modes                        string
			}
			if err := gossh.Unmarshal(request.Payload, &payload); err != nil {
				return err
			}
			events.initial <- WindowSize{Width: int(payload.Columns), Height: int(payload.Rows)}
			_ = request.Reply(true, nil)
		case "shell":
			_ = request.Reply(true, nil)
			_, _ = channel.Write([]byte("server-output"))
		case "window-change":
			var payload struct {
				Columns, Rows, Width, Height uint32
			}
			if err := gossh.Unmarshal(request.Payload, &payload); err != nil {
				return err
			}
			events.resize <- WindowSize{Width: int(payload.Columns), Height: int(payload.Rows)}
		}
	}
	return nil
}

func startTestSSH(t *testing.T, signer gossh.Signer, events testServerEvents) (net.Conn, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	accepted := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		close(accepted)
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		done <- serveTestSSH(connection, signer, events)
	}()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	<-accepted
	_ = listener.Close()
	return connection, done
}

func TestRunPinnedInteractiveSession(t *testing.T) {
	signer := testSigner(t)
	events := testServerEvents{
		input: make(chan string, 1), resize: make(chan WindowSize, 1),
		initial: make(chan WindowSize, 1), user: make(chan string, 1),
	}
	clientConnection, serverDone := startTestSSH(t, signer, events)

	ctx, cancel := context.WithCancel(context.Background())
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	output := new(testBuffer)
	resizes := make(chan WindowSize, 1)
	connected := make(chan struct{})
	type result struct {
		connected bool
		err       error
	}
	runDone := make(chan result, 1)
	go func() {
		attached, err := Run(ctx, clientConnection, Config{
			ExpectedHostKey: signer.PublicKey(),
			ClientAuth:      bytes.Repeat([]byte{0x42}, 32),
			InitialSize:     WindowSize{Width: 80, Height: 24},
			Input:           inputReader,
			Output:          output,
			ErrorOutput:     output,
			Resizes:         resizes,
			OnConnected:     func() { close(connected) },
		})
		runDone <- result{connected: attached, err: err}
	}()

	select {
	case <-connected:
	case err := <-serverDone:
		t.Fatalf("SSH server stopped before connection: %v", err)
	case got := <-runDone:
		t.Fatalf("SSH client stopped before connection: %v", got.err)
	case <-time.After(2 * time.Second):
		t.Fatal("SSH client did not connect")
	}
	if got := <-events.user; got != "croc" {
		t.Fatalf("SSH user = %q, want croc", got)
	}
	if got := <-events.initial; got != (WindowSize{Width: 80, Height: 24}) {
		t.Fatalf("initial terminal size = %+v", got)
	}
	if _, err := inputWriter.Write([]byte("browser-input")); err != nil {
		t.Fatal(err)
	}
	resizes <- WindowSize{Width: 132, Height: 41}
	if got := <-events.input; got != "browser-input" {
		t.Fatalf("server input = %q", got)
	}
	if got := <-events.resize; got != (WindowSize{Width: 132, Height: 41}) {
		t.Fatalf("resized terminal = %+v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(output.String(), "server-output") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(output.String(), "server-output") {
		t.Fatal("client did not receive terminal output")
	}
	cancel()
	got := <-runDone
	if !got.connected || !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Run() = (%v, %v), want connected context cancellation", got.connected, got.err)
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("SSH server did not stop after cancellation")
	}

	wrongSigner := testSigner(t)
	wrongEvents := testServerEvents{
		input: make(chan string, 1), resize: make(chan WindowSize, 1),
		initial: make(chan WindowSize, 1), user: make(chan string, 1),
	}
	wrongClient, _ := startTestSSH(t, signer, wrongEvents)
	attached, err := Run(t.Context(), wrongClient, Config{
		ExpectedHostKey: wrongSigner.PublicKey(), ClientAuth: bytes.Repeat([]byte{0x42}, 32), Input: bytes.NewReader(nil),
		Output: io.Discard, ErrorOutput: io.Discard,
	})
	if attached || err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Run() with wrong host key = (%v, %v)", attached, err)
	}
}
