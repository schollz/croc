// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build js

package tailcat

import (
	"context"

	"tailscale.com/tailcfg"
)

// PickBestRegion is a stub for js/wasm, where netcheck's STUN probes
// are unavailable (browsers can't send raw UDP). It always returns 0,
// which makes [ConnInfo.Expand] fall back to picking an arbitrary
// region, relying on the DERP map server to have already filtered the
// regions by client proximity.
func PickBestRegion(ctx context.Context, dm *tailcfg.DERPMap) (regionID int, err error) {
	return 0, nil
}
