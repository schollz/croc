package croc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/models"
	"github.com/stretchr/testify/require"
)

// These transfers require a SOCKS domain-name CONNECT request: only the proxy
// knows how to route the relay name, and client DNS is deliberately unavailable.
func TestSOCKS5TransferWithoutClientDNS(t *testing.T) {
	oldResolver, oldSOCKS, oldHTTP, oldInternalDNS := net.DefaultResolver, comm.Socks5Proxy, comm.HttpProxy, models.INTERNAL_DNS
	t.Cleanup(func() {
		net.DefaultResolver, comm.Socks5Proxy, comm.HttpProxy, models.INTERNAL_DNS = oldResolver, oldSOCKS, oldHTTP, oldInternalDNS
	})
	var dnsCalls atomic.Int32
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		dnsCalls.Add(1)
		return nil, errors.New("client DNS disabled")
	}}
	// Verify the test's DNS failure rather than relying on the machine's setup.
	_, err := net.DefaultResolver.LookupHost(t.Context(), "relay.socks-test.invalid")
	require.Error(t, err)
	require.Positive(t, dnsCalls.Swap(0))
	comm.HttpProxy = ""

	for _, tt := range []struct {
		name, scheme, relay string
		internalDNS         bool
	}{
		{"bare/default-ipv4", "", models.DEFAULT_RELAY, false},
		{"socks5/default-ipv6", "socks5://", models.DEFAULT_RELAY6, false},
		{"socks5h/custom/internal-dns", "socks5h://", "relay.socks-test.invalid:9009", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			relay := startTestRelay(t, 2)
			host, controlPort, err := net.SplitHostPort(tt.relay)
			require.NoError(t, err)
			require.Nil(t, net.ParseIP(host), "relay must remain a hostname")
			proxyAddress, requests := startSOCKS5TestProxy(t, host, controlPort, relay)
			comm.Socks5Proxy = tt.scheme + proxyAddress
			models.INTERNAL_DNS = tt.internalDNS

			sourceDir, receiveDir := t.TempDir(), t.TempDir()
			t.Chdir(receiveDir)
			payload := bytes.Repeat([]byte("SOCKS5 remote DNS transfer\n"), 8192)
			source := filepath.Join(sourceDir, "payload.txt")
			require.NoError(t, os.WriteFile(source, payload, 0600))
			files, folders, folderCount, err := GetFilesInfo([]string{source}, false, false, nil)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			options := Options{
				IsSender: true, SharedSecret: "socks-remote-dns", RelayAddress: tt.relay,
				RelayPassword: "pass123", NoPrompt: true, DisableLocal: true,
				Curve: "siec", Overwrite: true, Transport: TransportRelay,
			}
			sender, err := NewCtx(ctx, options)
			require.NoError(t, err)
			options.IsSender, options.Transport = false, TransportAuto
			receiver, err := NewCtx(ctx, options)
			require.NoError(t, err)
			defer cancelClients(sender, receiver)
			results := make(chan error, 2)
			go func() { results <- sender.Send(files, folders, folderCount) }()
			go func() { results <- receiver.Receive() }()
			waitForTransferResults(t, results, 2, 15*time.Second, sender, receiver)
			received, err := os.ReadFile(filepath.Join(receiveDir, "payload.txt"))
			require.NoError(t, err)
			require.Equal(t, payload, received)
			require.Zero(t, dnsCalls.Load(), "transfer must not query client DNS")
			seen := requests()
			require.GreaterOrEqual(t, seen[controlPort], 2, "both peers must proxy the control connection")
			for _, port := range relay.dataPorts {
				require.GreaterOrEqual(t, seen[port], 2, "both peers must proxy data port %s", port)
			}
		})
	}
}

// startSOCKS5TestProxy maps one remote hostname to a loopback test relay. It
// rejects IP CONNECT requests and unexpected destinations instead of resolving
// them, so a successful transfer proves DNS was delegated to the proxy.
func startSOCKS5TestProxy(t *testing.T, host, controlPort string, relay testRelay) (string, func() map[string]int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	seen := make(map[string]int)
	var wg sync.WaitGroup
	handle := func(client net.Conn) error {
		defer client.Close()
		if err := client.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
			return err
		}
		var greeting [2]byte
		if _, err := io.ReadFull(client, greeting[:]); err != nil {
			return err
		}
		if greeting[0] != 5 {
			return fmt.Errorf("unexpected SOCKS version: %d", greeting[0])
		}
		methods := make([]byte, int(greeting[1]))
		if _, err := io.ReadFull(client, methods); err != nil {
			return err
		}
		if !bytes.Contains(methods, []byte{0}) {
			return errors.New("no anonymous authentication offered")
		}
		if _, err := client.Write([]byte{5, 0}); err != nil {
			return err
		}
		var header [5]byte
		if _, err := io.ReadFull(client, header[:]); err != nil {
			return err
		}
		if !bytes.Equal(header[:4], []byte{5, 1, 0, 3}) {
			return fmt.Errorf("expected domain-name CONNECT, got %v", header)
		}
		target := make([]byte, int(header[4])+2)
		if _, err := io.ReadFull(client, target); err != nil {
			return err
		}
		if string(target[:len(target)-2]) != host {
			return fmt.Errorf("unexpected relay hostname %q", target[:len(target)-2])
		}
		port := strconv.Itoa(int(binary.BigEndian.Uint16(target[len(target)-2:])))
		localPort := port
		if port == controlPort {
			localPort = relay.controlPort
		}
		allowed := localPort == relay.controlPort
		for _, dataPort := range relay.dataPorts {
			allowed = allowed || localPort == dataPort
		}
		if !allowed {
			return fmt.Errorf("unexpected relay port %s", port)
		}
		upstream, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", localPort), time.Second)
		if err != nil {
			return err
		}
		defer upstream.Close()
		if _, err := client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
			return err
		}
		mu.Lock()
		seen[port]++
		mu.Unlock()
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
		return nil
	}
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				if err := handle(conn); err != nil {
					t.Errorf("SOCKS5 proxy: %v", err)
				}
			})
		}
	})
	t.Cleanup(func() { _ = listener.Close(); wg.Wait() })
	return listener.Addr().String(), func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		result := make(map[string]int, len(seen))
		maps.Copy(result, seen)
		return result
	}
}
