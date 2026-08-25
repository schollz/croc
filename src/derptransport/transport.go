// Package derptransport adapts derphole's one-shot Attach sessions into the
// net.Conn data path used by croc.
package derptransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/shayne/derphole/pkg/derpbind"
	"github.com/shayne/derphole/pkg/token"
)

const MaxTokenSize = 2048

var (
	ErrCustomRoute = errors.New("custom DERP routes are not supported by the DERP transport")
	ErrTokenSize   = errors.New("DERP offer token has an invalid size")
	ErrCapability  = errors.New("DERP offer token does not permit an Attach session")
	ErrUnsupported = errors.New("DERP transport is unavailable on this platform")
)

// PathEvent reports derphole transport state without exposing session secrets.
type PathEvent func(string)

const DefaultGroupStreams = 8
const DefaultGroupRawPaths = 4

type GroupConfig struct {
	StreamCount     int
	MaxRawPaths     int
	RawDirectBudget time.Duration
	ForceRelay      bool
}

func DefaultGroupConfig() GroupConfig {
	return GroupConfig{
		StreamCount:     DefaultGroupStreams,
		MaxRawPaths:     DefaultGroupRawPaths,
		RawDirectBudget: 3 * time.Second,
	}
}

func normalizeGroupConfig(cfg GroupConfig) GroupConfig {
	defaults := DefaultGroupConfig()
	if cfg.StreamCount == 0 {
		cfg.StreamCount = defaults.StreamCount
	}
	if cfg.MaxRawPaths == 0 {
		cfg.MaxRawPaths = defaults.MaxRawPaths
	}
	if cfg.RawDirectBudget == 0 {
		cfg.RawDirectBudget = defaults.RawDirectBudget
	}
	return cfg
}

// Listener owns a one-shot derphole Attach listener.
type Listener interface {
	Token() string
	Accept(context.Context) (net.Conn, error)
	Close() error
}

type BundleStats struct {
	Mode              string
	Path              string
	StreamCount       int
	RawPathCount      int
	SetupDuration     time.Duration
	RawSetupDuration  time.Duration
	FallbackDuration  time.Duration
	FallbackReason    string
	CandidateDuration time.Duration
	ExchangeDuration  time.Duration
	PunchDuration     time.Duration
	SelectionDuration time.Duration
	HandshakeDuration time.Duration
	ReadinessDuration time.Duration
	BytesSent         int64
	BytesReceived     int64
	PacketsSent       uint64
	PacketsReceived   uint64
	PacketsLost       uint64
	WireBytesSent     uint64
	RecoveryWireBytes uint64
	SmoothedRTT       time.Duration
}

type Bundle interface {
	Connections() []net.Conn
	Stats() BundleStats
	Close() error
}

type GroupListener interface {
	Token() string
	Accept(context.Context) (Bundle, error)
	Close() error
}

// ValidateToken checks the non-secret envelope properties needed by croc
// before handing an offer to derphole.
func ValidateToken(encoded string, now time.Time) error {
	return validateTokenCapabilities(encoded, now, token.CapabilityAttach)
}

// ValidateGroupToken additionally requires the additive AttachGroup capability.
func ValidateGroupToken(encoded string, now time.Time) error {
	return validateTokenCapabilities(encoded, now, token.CapabilityAttach|token.CapabilityAttachGroup)
}

func validateTokenCapabilities(encoded string, now time.Time, capabilities uint32) error {
	if len(encoded) == 0 || len(encoded) > MaxTokenSize {
		return ErrTokenSize
	}
	decoded, err := token.Decode(encoded, now)
	if err != nil {
		return fmt.Errorf("invalid DERP offer token: %w", err)
	}
	if decoded.Capabilities&capabilities != capabilities {
		return ErrCapability
	}
	if decoded.DERPRoute.IsCustom() {
		return ErrCustomRoute
	}
	return nil
}

// ValidatePublicRoute rejects derphole's custom-server escape hatch. This
// transport intentionally uses only the public Tailscale DERP map.
func ValidatePublicRoute() error {
	if value, ok := os.LookupEnv(derpbind.CustomDERPServerEnv); ok && value != "" {
		return ErrCustomRoute
	}
	return nil
}
