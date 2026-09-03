//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

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
