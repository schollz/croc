// Package tailcattransport binds croc's authenticated session key to an
// in-process Tailcat data plane.
package tailcattransport

import (
	"errors"
	"fmt"
	"time"

	"tailscale.com/tailcfg"
)

const (
	MinStreamCount = 1
	MaxStreamCount = 16
	MaxOfferSize   = 4 * 1024
	FirstTCPPort   = 40000
)

var (
	ErrUnsupported  = errors.New("Tailcat transport is unavailable on this platform")
	ErrInvalidOffer = errors.New("invalid Tailcat offer")
)

// Config controls the number of parallel TCP streams in a transfer.
type Config struct {
	StreamCount int
}

// Prepared is an opaque, session-independent Tailcat bootstrap selection.
// It deliberately contains no node identity or transfer key material.
type Prepared struct {
	region *tailcfg.DERPRegion
}

// Validate checks that the configured stream count is supported.
func (c Config) Validate() error {
	if c.StreamCount < MinStreamCount || c.StreamCount > MaxStreamCount {
		return fmt.Errorf("Tailcat stream count must be between %d and %d", MinStreamCount, MaxStreamCount)
	}
	return nil
}

// PathEvent receives sanitized transport lifecycle and path changes.
type PathEvent func(string)

// BundleStats is a final or point-in-time Tailcat connection summary.
type BundleStats struct {
	Path          string
	StreamCount   int
	SetupDuration time.Duration
	BytesSent     int64
	BytesReceived int64
}
