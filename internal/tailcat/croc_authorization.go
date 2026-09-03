package tailcat

import (
	"net/netip"

	"tailscale.com/types/key"
)

// AddrForNodeKey returns the private Tailcat address assigned to a node key.
// Croc's SSH sharing layer uses it to bind a PAKE-authorized key to the access
// role enforced when that peer opens an SSH connection.
func AddrForNodeKey(k key.NodePublic) netip.Addr {
	return tcAddrForKey(k)
}
