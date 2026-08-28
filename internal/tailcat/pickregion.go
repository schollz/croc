// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build !js

package tailcat

import (
	"context"
	"fmt"
	"log"
	"time"

	"tailscale.com/net/netcheck"
	"tailscale.com/net/netmon"
	"tailscale.com/tailcfg"
	"tailscale.com/types/logger"
)

// PickBestRegion runs a netcheck over the DERP regions in dm and
// returns the region ID with the lowest latency. It returns 0 (and a
// nil error) if the netcheck report contained no usable region
// latencies.
func PickBestRegion(ctx context.Context, dm *tailcfg.DERPMap) (regionID int, err error) {
	nc := &netcheck.Client{
		NetMon:  netmon.NewStatic(),
		Verbose: Verbose,
		Logf:    logger.Discard,
	}
	if Verbose {
		nc.Logf = log.Printf
	}
	if err := nc.Standalone(ctx, ":0"); err != nil {
		return 0, fmt.Errorf("netcheck.Standalone: %w", err)
	}
	t0 := time.Now()
	nr, err := nc.GetReport(ctx, dm, &netcheck.GetReportOpts{})
	if err != nil {
		return 0, fmt.Errorf("failed to get netcheck report: %w", err)
	}
	if Verbose {
		log.Printf("Got netcheck after %v: %v", time.Since(t0), logger.AsJSON(nr))
	}
	bestLatency := time.Hour
	for rid, d := range nr.RegionLatency {
		if _, ok := dm.Regions[rid]; !ok {
			continue // shouldn't happen
		}
		if d < bestLatency {
			bestLatency = d
			regionID = rid
		}
	}
	return regionID, nil
}
