//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ssh "github.com/tailscale/gliderssh"
	gossh "golang.org/x/crypto/ssh"
)

type sshTestClient struct {
	client  *gossh.Client
	session *gossh.Session
	stdin   io.WriteCloser
	output  *lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func newTestSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := gossh.NewSignerFromKey(private)
	require.NoError(t, err)
	return signer
}

func connectTestSSH(t *testing.T, server *ssh.Server, signer gossh.Signer) *sshTestClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	accepted := make(chan struct{})
	go func() {
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			close(accepted)
			server.HandleConn(serverConn)
		}
	}()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	<-accepted
	require.NoError(t, listener.Close())
	config := &gossh.ClientConfig{
		User: "test",
		HostKeyCallback: func(_ string, _ net.Addr, key gossh.PublicKey) error {
			require.Equal(t, signer.PublicKey().Marshal(), key.Marshal())
			return nil
		},
	}
	connection, channels, requests, err := gossh.NewClientConn(clientConn, "test", config)
	require.NoError(t, err)
	client := gossh.NewClient(connection, channels, requests)
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NoError(t, session.RequestPty("xterm-256color", 24, 80, nil))
	stdin, err := session.StdinPipe()
	require.NoError(t, err)
	output := new(lockedBuffer)
	session.Stdout = output
	session.Stderr = output
	require.NoError(t, session.Shell())
	result := &sshTestClient{client: client, session: session, stdin: stdin, output: output}
	t.Cleanup(func() {
		_ = result.stdin.Close()
		_ = result.session.Close()
		_ = result.client.Close()
	})
	return result
}

func TestSharedSSHIsMultiUserReadOnlyAndReconnectable(t *testing.T) {
	pty := newMemoryPTY()
	hub := newTerminalHub(t.Context(), pty, nil, nil)
	t.Cleanup(func() { _ = hub.Close() })
	signer := newTestSigner(t)
	rwServer := newSharedSSHServer(hub, signer, RoleReadWrite, nil)
	roServer := newSharedSSHServer(hub, signer, RoleReadOnly, nil)

	rw := connectTestSSH(t, rwServer, signer)
	ro := connectTestSSH(t, roServer, signer)
	pty.reads <- []byte("shared-output\r\n")
	require.Eventually(t, func() bool {
		return bytes.Contains([]byte(rw.output.String()), []byte("shared-output")) &&
			bytes.Contains([]byte(ro.output.String()), []byte("shared-output"))
	}, 2*time.Second, time.Millisecond)

	_, err := rw.stdin.Write([]byte("writer-input"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return pty.written() == "writer-input"
	}, time.Second, time.Millisecond)
	_, err = ro.stdin.Write([]byte("read-only-input"))
	require.NoError(t, err)
	time.Sleep(25 * time.Millisecond)
	require.Equal(t, "writer-input", pty.written())

	_ = ro.stdin.Close()
	_ = ro.session.Close()
	_ = ro.client.Close()
	reconnected := connectTestSSH(t, roServer, signer)
	require.Eventually(t, func() bool {
		return bytes.Contains([]byte(reconnected.output.String()), []byte("shared-output"))
	}, 2*time.Second, time.Millisecond)
}
