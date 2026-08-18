// Package publicrelay defines and measures croc's ordered public relay pool.
package publicrelay

import (
	"context"
	"errors"
	"time"
)

const ProbeTimeout = time.Second

var relays = []string{
	"1.getcroc.com:9009",
	"2.getcroc.com:9009",
	"3.getcroc.com:9009",
}

// Relays returns a copy of the protocol-ordered public relay pool.
func Relays() []string {
	return append([]string(nil), relays...)
}

// Probe measures one complete croc ping/pong exchange.
type Probe func(ctx context.Context, address string, timeout time.Duration) (time.Duration, error)

type probeResult struct {
	index    int
	duration time.Duration
	err      error
}

// SelectFirst races one probe to every relay and returns as soon as the first
// relay completes a successful croc ping/pong exchange. Outstanding probes are
// canceled before returning.
func SelectFirst(ctx context.Context, addresses []string, timeout time.Duration, probe Probe) (int, time.Duration, error) {
	if len(addresses) == 0 {
		return 0, 0, errors.New("public relay pool is empty")
	}
	if timeout <= 0 {
		return 0, 0, errors.New("probe timeout must be positive")
	}
	if probe == nil {
		return 0, 0, errors.New("relay probe is required")
	}
	if ctx == nil {
		return 0, 0, errors.New("probe context is required")
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan probeResult, len(addresses))
	for index, address := range addresses {
		go func() {
			duration, err := probe(probeCtx, address, timeout)
			results <- probeResult{index: index, duration: duration, err: err}
		}()
	}

	for range addresses {
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case result := <-results:
			if result.err == nil {
				return result.index, result.duration, nil
			}
		}
	}
	return 0, 0, errors.New("no public relay available")
}
