//go:build !js

package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func localRelay(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(executable, "-test.run=^TestTunnelForwardingIntegration$")
	child.Env = append(os.Environ(), "CROC_TUNNEL_TEST_RELAY="+strconv.Itoa(port))
	require.NoError(t, child.Start())
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)
	return address
}
func echoService(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				body, _ := io.ReadAll(connection)
				_, _ = connection.Write(append([]byte("reply:"), body...))
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done; wg.Wait() })
	return listener
}
func TestTunnelForwardingIntegration(t *testing.T) {
	if port := os.Getenv("CROC_TUNNEL_TEST_RELAY"); port != "" {
		if err := tcp.RunWithOptionsAsync("127.0.0.1", port, "testpass", tcp.WithLogLevel("error"), tcp.WithAdmissionLimits(1000, 1000, time.Minute)); err != nil {
			os.Exit(2)
		}
		select {}
	}
	relay := localRelay(t)
	upstream := echoService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	host, err := StartHost(ctx, HostConfig{Port: upstream.Addr().(*net.TCPAddr).Port, RelayAddress: relay, RelayPassword: "testpass"})
	require.NoError(t, err)
	defer host.Close()
	config := ClientConfig{Code: host.Code(), RelayAddress: relay, RelayPassword: "testpass"}
	a, err := Join(ctx, config)
	require.NoError(t, err)
	defer a.Close()
	b, err := Join(ctx, config)
	require.NoError(t, err)
	defer b.Close()
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			peer := a
			if i%2 == 1 {
				peer = b
			}
			ch, err := peer.OpenTCP(ctx)
			if err != nil {
				failures <- err
				return
			}
			defer ch.Close()
			payload := bytes.Repeat([]byte{byte(i)}, 100000)
			_, err = ch.Write(payload)
			if err == nil {
				err = ch.CloseWrite()
			}
			var received []byte
			if err == nil {
				received, err = io.ReadAll(ch)
			}
			if err == nil && !bytes.Equal(received, append([]byte("reply:"), payload...)) {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	// Fresh authorization after disconnect reaches the same host.
	require.NoError(t, a.Close())
	a, err = Join(ctx, config)
	require.NoError(t, err)
	defer a.Close()
	// Listener forwarding has the same half-close behavior and is loopback only.
	local, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := local.Addr().(*net.TCPAddr).Port
	_ = local.Close()
	forwardCtx, stopForward := context.WithCancel(ctx)
	forwardDone := make(chan error, 1)
	ready := make(chan struct{}, 1)
	go func() {
		forwardDone <- Forward(forwardCtx, config, port, func(string) {
			select {
			case ready <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	connection, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	_, err = connection.Write([]byte("forwarded"))
	require.NoError(t, err)
	require.NoError(t, connection.(*net.TCPConn).CloseWrite())
	body, err := io.ReadAll(connection)
	require.NoError(t, err)
	require.Equal(t, "reply:forwarded", string(body))
	_ = connection.Close()
	stopForward()
	<-forwardDone
	_ = upstream.Close()
	ch, err := b.OpenTCP(ctx)
	require.NoError(t, err)
	_, err = ch.Read(make([]byte, 1))
	require.Error(t, err)
	_ = ch.Close()
	require.NoError(t, host.Close())
	require.NoError(t, a.Wait())
	require.NoError(t, b.Wait())
}
func TestTunnelAuthorizationBoundaries(t *testing.T) {
	// Exercise actual host authorization, including wrong invitations and a
	// different advertised key, over real socket pairs without a second relay.
	upstream := echoService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	host, err := StartHost(ctx, HostConfig{Port: upstream.Addr().(*net.TCPAddr).Port, RelayAddress: "127.0.0.1:1", RelayPassword: "unused"})
	require.NoError(t, err)
	defer host.Close()
	connect := func(code string, wrongPin bool) (*Session, error) {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, e)
		defer listener.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			defer connection.Close()
			credential, e := host.authorize(comm.New(connection), strings.Repeat("a", 64))
			_ = connection.Close()
			if e == nil {
				data, err := listener.Accept()
				if err == nil {
					defer data.Close()
					if prepareStream(data) == nil {
						host.serve(data, credential, time.Now().Add(authTimeout))
					}
				}
			}
		}()
		raw, e := net.Dial("tcp", listener.Addr().String())
		require.NoError(t, e)
		t.Cleanup(func() { _ = raw.Close(); <-done })
		if !wrongPin {
			return Authenticate(ctx, raw, code, "p256", func(context.Context, string) (net.Conn, error) { return net.Dial("tcp", listener.Addr().String()) })
		}
		components, _ := codephrase.ParseTunnel(code)
		c := comm.New(raw)
		key, deadline, e := guestPAKE(c, components, "p256")
		if e != nil {
			return nil, e
		}
		credential := bytes.Repeat([]byte{1}, 32)
		e = sendMessageUntil(c, key, message.Message{Type: "tunnel-authorize", Version: protocolVersion, Bytes: credential}, deadline)
		if e != nil {
			return nil, e
		}
		_, e = receiveMessageUntil(c, key, deadline)
		if e != nil {
			return nil, e
		}
		// A valid but different Ed25519 key must fail pinning.
		signer, e := newTestSigner()
		if e != nil {
			return nil, e
		}
		_ = raw.Close()
		data, e := net.Dial("tcp", listener.Addr().String())
		if e != nil {
			return nil, e
		}
		defer data.Close()
		if e := prepareStream(data); e != nil {
			return nil, e
		}
		return connectSSH(ctx, data, signer.PublicKey(), credential, host.config.Port)
	}
	t.Run("wrong invitation", func(t *testing.T) {
		components, err := codephrase.ParseTunnel(host.Code())
		require.NoError(t, err)
		secret := "acid-acid-acid-acid"
		if secret == components.PAKEPassphrase {
			secret = "acorn-acorn-acorn-acorn"
		}
		wrong := strings.TrimSuffix(host.Code(), components.PAKEPassphrase) + secret
		wrongComponents, err := codephrase.ParseTunnel(wrong)
		require.NoError(t, err)
		require.Equal(t, components.RoomName, wrongComponents.RoomName)
		_, err = connect(wrong, false)
		require.Error(t, err)
	})
	t.Run("expired and consumed credential", func(t *testing.T) {
		credential := bytes.Repeat([]byte{7}, 32)
		password := []byte(base64.RawStdEncoding.EncodeToString(credential))
		expired := host.sshConfig(credential, time.Now().Add(-time.Second))
		_, err := expired.PasswordCallback(nil, password)
		require.Error(t, err)
		active := host.sshConfig(credential, time.Now().Add(time.Minute))
		_, err = active.PasswordCallback(nil, []byte("invalid"))
		require.Error(t, err)
		_, err = active.PasswordCallback(nil, password)
		require.NoError(t, err)
		_, err = active.PasswordCallback(nil, password)
		require.Error(t, err)
	})
	t.Run("host key pin", func(t *testing.T) {
		_, err := connect(host.Code(), true)
		require.ErrorContains(t, err, "host key does not match")
	})
	peer, err := connect(host.Code(), false)
	require.NoError(t, err)
	defer peer.Close()
	for _, kind := range []string{"session", "direct-tcpip", "forwarded-tcpip"} {
		ch, _, err := peer.conn.OpenChannel(kind, nil)
		if ch != nil {
			_ = ch.Close()
		}
		require.Error(t, err)
	}
	for _, path := range []string{"http://example.com/", "//example.com/", "/bad\r\nHost: example.com", "/\\example.com"} {
		metadata, err := json.Marshal(BrowserRequest{Path: path})
		require.NoError(t, err)
		channel, _, err := peer.conn.OpenChannel(ChannelHTTP, metadata)
		if channel != nil {
			_ = channel.Close()
		}
		require.Error(t, err)
	}
	_, _, err = ReadRecord(bytes.NewReader([]byte{RecordData, 255, 255, 255, 255}))
	require.Error(t, err)
	var header [5]byte
	header[0] = RecordData
	binary.BigEndian.PutUint32(header[1:], MaxRecord+1)
	_, _, err = ReadRecord(bytes.NewReader(header[:]))
	require.Error(t, err)
	tunnelCode, _ := codephrase.ParseTunnel(host.Code())
	sshCode, _ := codephrase.ParseSSH(host.Code())
	require.NotEqual(t, tunnelCode.RoomName, sshCode.RoomName)
}

func newTestSigner() (ssh.Signer, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}
