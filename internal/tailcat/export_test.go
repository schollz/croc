// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailcat

import (
	"context"
	"testing"
	"time"
)

// WaitForDERPForTest waits for the server's and client's DERP connections.
// Integration tests should use this instead of guessing how long startup takes.
func WaitForDERPForTest(t testing.TB, s *Server, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.ensureStarted(ctx); err != nil {
		t.Fatalf("starting client: %v", err)
	}

	for name, lb := range map[string]*locoBackend{"server": s.lb, "client": c.lb} {
		regionID := lb.derpRegionID()
		for lb.sys.HealthTracker.Get().GetDERPRegionReceivedTime(regionID).IsZero() {
			select {
			case <-ctx.Done():
				t.Fatalf("waiting for %s DERP connection: %v", name, ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
	}
}

// PingForTest waits for the client's DERP connection and performs the
// meow/meowed handshake.
func PingForTest(t testing.TB, s *Server, c *Client) PingResult {
	t.Helper()
	WaitForDERPForTest(t, s, c)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	return res
}
