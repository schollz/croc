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

// Listener owns a one-shot derphole Attach listener.
type Listener interface {
	Token() string
	Accept(context.Context) (net.Conn, error)
	Close() error
}

// ValidateToken checks the non-secret envelope properties needed by croc
// before handing an offer to derphole.
func ValidateToken(encoded string, now time.Time) error {
	if len(encoded) == 0 || len(encoded) > MaxTokenSize {
		return ErrTokenSize
	}
	decoded, err := token.Decode(encoded, now)
	if err != nil {
		return fmt.Errorf("invalid DERP offer token: %w", err)
	}
	if decoded.Capabilities&token.CapabilityAttach == 0 {
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
