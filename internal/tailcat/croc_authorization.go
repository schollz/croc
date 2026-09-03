package tailcat

import (
	"net/netip"
	"slices"

	"tailscale.com/types/key"
)

// AddrForNodeKey returns the private Tailcat address assigned to a node key.
// Croc's SSH sharing layer uses it to bind a PAKE-authorized key to the access
// role enforced when that peer opens an SSH connection.
func AddrForNodeKey(k key.NodePublic) netip.Addr {
	return tcAddrForKey(k)
}

// RemoveAllowedClient revokes a client previously admitted with
// AddAllowedClient or AllowedClients. Existing transport connections are not
// forcibly closed, but the key can no longer establish a new Tailcat peer.
func (s *Server) RemoveAllowedClient(k key.NodePublic) {
	if s.lb == nil {
		s.AllowedClients = slices.DeleteFunc(s.AllowedClients, func(candidate key.NodePublic) bool {
			return candidate == k
		})
		return
	}
	s.lb.mu.Lock()
	defer s.lb.mu.Unlock()
	delete(s.lb.allowedClients, k)
}
