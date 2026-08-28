// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailcat

import "tailscale.com/ipn/ipnstate"

// Status returns the client's current WireGuard and magicsock status.
// It returns nil until the client has initialized its networking stack.
func (c *Client) Status() *ipnstate.Status {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.lb == nil {
		return nil
	}
	return c.lb.Status()
}
