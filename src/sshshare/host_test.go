//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/key"
)

func TestRoleGrantCannotEscalateReadOnlyPort(t *testing.T) {
	host := &Host{grants: make(map[netip.Addr]roleGrant)}
	address := netip.MustParseAddr("fd7a:115c:a1e0::1234")
	host.grants[address] = roleGrant{role: RoleReadOnly, expiresAt: time.Now().Add(time.Minute)}
	_, ok := host.consumeGrant(address, RoleReadWrite)
	require.False(t, ok)
	_, ok = host.consumeGrant(address, RoleReadOnly)
	require.True(t, ok)
}

func TestExpiredRoleGrantIsRejected(t *testing.T) {
	host := &Host{grants: make(map[netip.Addr]roleGrant)}
	address := netip.MustParseAddr("fd7a:115c:a1e0::1234")
	host.grants[address] = roleGrant{role: RoleReadWrite, expiresAt: time.Now().Add(-time.Second)}
	_, ok := host.consumeGrant(address, RoleReadWrite)
	require.False(t, ok)
	require.Empty(t, host.grants)
}

func TestExpireGrantsRevokesTailcatAllowlist(t *testing.T) {
	clientKey := key.NewNode().Public()
	address := tailcat.AddrForNodeKey(clientKey)
	server := &tailcat.Server{AllowedClients: []key.NodePublic{clientKey}}
	host := &Host{
		server: server,
		grants: map[netip.Addr]roleGrant{
			address: {clientKey: clientKey, role: RoleReadWrite, expiresAt: time.Now().Add(-time.Second)},
		},
	}
	host.expireGrants(time.Now())
	require.Empty(t, host.grants)
	require.Empty(t, server.AllowedClients)
}

func TestRoleGrantIsConsumedOnce(t *testing.T) {
	clientKey := key.NewNode().Public()
	host := &Host{grants: make(map[netip.Addr]roleGrant)}
	address := netip.MustParseAddr("fd7a:115c:a1e0::1234")
	host.grants[address] = roleGrant{
		clientKey: clientKey,
		role:      RoleReadWrite,
		expiresAt: time.Now().Add(time.Minute),
	}
	got, ok := host.consumeGrant(address, RoleReadWrite)
	require.True(t, ok)
	require.Equal(t, clientKey, got)
	_, ok = host.consumeGrant(address, RoleReadWrite)
	require.False(t, ok)
}

func TestRemoteIP(t *testing.T) {
	want := netip.MustParseAddr("fd7a:115c:a1e0::1234")
	got, ok := remoteIP(&net.TCPAddr{IP: net.ParseIP(want.String()), Port: 123})
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestRelayForCodeIsStable(t *testing.T) {
	code := "acid-acorn-acre-acts-ahead-alien"
	first, err := relayForCode("", code)
	require.NoError(t, err)
	second, err := relayForCode("", code)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEmpty(t, first)

	explicit, err := relayForCode("relay.example:9009", code)
	require.NoError(t, err)
	require.Equal(t, "relay.example:9009", explicit)
}

func TestHostRejectsCodesSharingRendezvousRoom(t *testing.T) {
	_, err := StartHost(t.Context(), HostConfig{
		ReadWriteCode: "acid-acorn-acre-acts-ahead-alien",
		ReadOnlyCode:  "acid-acorn-acre-acts-ahead-apron",
		RelayAddress:  "127.0.0.1:1",
		RelayPassword: "test",
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "different first two words"), err)
}

func TestHostRejectsNegativeAuthorizationTTL(t *testing.T) {
	_, err := StartHost(t.Context(), HostConfig{
		RelayPassword:    "test",
		AuthorizationTTL: -time.Second,
	})
	require.ErrorContains(t, err, "must not be negative")
}

func TestStartHostUsesDefaultAuthorizationTTLAndClosesWorkers(t *testing.T) {
	transport := &tailcat.Server{}
	host, err := startHostWithDeps(t.Context(), HostConfig{
		RelayAddress:  "relay.example:9009",
		RelayPassword: "test",
	}, hostDeps{
		startTerminal: func(ctx context.Context, _ []string, _ string, _ WindowSize) (*terminalHub, error) {
			return newTerminalHub(ctx, newMemoryPTY(), nil, nil), nil
		},
		startTransport: func(_ context.Context, _ HostConfig, handler func(uint16) func(net.Conn)) (hostTransport, string, error) {
			require.NotNil(t, handler(readWritePort))
			require.NotNil(t, handler(readOnlyPort))
			return transport, "tailcat-offer", nil
		},
		connect: func(ctx context.Context, _, _, _ string, _ time.Duration) (*comm.Comm, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	require.NoError(t, err)
	require.Equal(t, defaultAuthorizationTTL, host.config.AuthorizationTTL)
	require.NotEmpty(t, host.Code(RoleReadWrite))
	require.NotEmpty(t, host.Code(RoleReadOnly))
	require.NoError(t, host.Close())
	select {
	case <-host.Done():
	default:
		t.Fatal("Host.Close did not stop every worker")
	}
}

func TestStartHostCleansUpTerminalWhenTransportFails(t *testing.T) {
	pty := newMemoryPTY()
	startErr := errors.New("start transport")
	_, err := startHostWithDeps(t.Context(), HostConfig{
		RelayAddress:  "relay.example:9009",
		RelayPassword: "test",
	}, hostDeps{
		startTerminal: func(ctx context.Context, _ []string, _ string, _ WindowSize) (*terminalHub, error) {
			return newTerminalHub(ctx, pty, nil, nil), nil
		},
		startTransport: func(context.Context, HostConfig, func(uint16) func(net.Conn)) (hostTransport, string, error) {
			return nil, "", startErr
		},
	})
	require.ErrorIs(t, err, startErr)
	select {
	case <-pty.closed:
	default:
		t.Fatal("terminal remained open after transport startup failed")
	}
}

type blockingSSHServer struct {
	started     chan struct{}
	released    chan struct{}
	closed      chan struct{}
	startedOnce sync.Once
	closeOnce   sync.Once
	closeErr    error
}

func (s *blockingSSHServer) HandleConn(net.Conn) {
	s.startedOnce.Do(func() { close(s.started) })
	<-s.closed
	close(s.released)
}

func (s *blockingSSHServer) AddClientAuth([]byte, time.Time) error { return nil }

func (s *blockingSSHServer) RevokeClientAuth([]byte) {}

func (s *blockingSSHServer) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return s.closeErr
}

func TestHostCloseWaitsForSessionsAndReturnsStableError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	closeErr := errors.New("close SSH server")
	sshServer := &blockingSSHServer{
		started: make(chan struct{}), released: make(chan struct{}), closed: make(chan struct{}), closeErr: closeErr,
	}
	hub := newTerminalHub(ctx, newMemoryPTY(), nil, nil)
	host := &Host{
		ctx: ctx, cancel: cancel, hub: hub,
		invitations: map[Role]*invitation{RoleReadWrite: {sshServer: sshServer}},
		grants:      make(map[netip.Addr]roleGrant),
		done:        make(chan struct{}),
	}
	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() { _ = peerConn.Close() })
	host.serveRelaySSH(comm.New(clientConn), host.invitations[RoleReadWrite])
	select {
	case <-sshServer.started:
	case <-time.After(time.Second):
		t.Fatal("SSH session did not start")
	}

	first := host.Close()
	require.ErrorIs(t, first, closeErr)
	select {
	case <-sshServer.released:
	default:
		t.Fatal("Host.Close returned before its SSH session stopped")
	}
	require.ErrorIs(t, host.Close(), closeErr)
	require.Equal(t, first, host.Close())
}

func TestBeginAttachmentTracksLifecycleAndRejectsClosingHost(t *testing.T) {
	var events []HostEvent
	host := &Host{config: HostConfig{OnEvent: func(event HostEvent) {
		events = append(events, event)
	}}}
	done := host.beginAttachment(RoleReadOnly)
	require.NotNil(t, done)

	waited := make(chan struct{})
	go func() {
		host.sessionWG.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("attachment was not included in the host session wait group")
	case <-time.After(20 * time.Millisecond):
	}
	done()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("attachment did not leave the host session wait group")
	}
	require.Equal(t, []HostEvent{
		{Role: RoleReadOnly, Connected: true, Attachments: 1},
		{Role: RoleReadOnly, Connected: false, Attachments: 0},
	}, events)

	host.sessionMu.Lock()
	host.closing = true
	host.sessionMu.Unlock()
	require.Nil(t, host.beginAttachment(RoleReadOnly))
}
