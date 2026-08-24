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
	"github.com/shayne/derphole/pkg/session"
	"github.com/shayne/derphole/pkg/telemetry"
	"github.com/shayne/derphole/pkg/token"
)

const MaxTokenSize = 2048

var (
	ErrCustomRoute = errors.New("custom DERP routes are not supported by --experimental-derp")
	ErrTokenSize   = errors.New("DERP offer token has an invalid size")
	ErrCapability  = errors.New("DERP offer token does not permit an Attach session")
)

// PathEvent reports derphole transport state without exposing session secrets.
type PathEvent func(string)

// Listener owns a one-shot derphole Attach listener.
type Listener interface {
	Token() string
	Accept(context.Context) (net.Conn, error)
	Close() error
}

type listener struct {
	inner *session.AttachListener
}

func (l *listener) Token() string {
	if l == nil || l.inner == nil {
		return ""
	}
	return l.inner.Token
}

func (l *listener) Accept(ctx context.Context) (net.Conn, error) {
	if l == nil || l.inner == nil {
		return nil, net.ErrClosed
	}
	return l.inner.Accept(ctx)
}

func (l *listener) Close() error {
	if l == nil || l.inner == nil {
		return net.ErrClosed
	}
	return l.inner.Close()
}

// Listen creates a public DERP Attach listener with direct-path promotion.
func Listen(ctx context.Context, events PathEvent) (Listener, error) {
	if err := ValidatePublicRoute(); err != nil {
		return nil, err
	}
	inner, err := session.ListenAttach(ctx, session.AttachListenConfig{
		Emitter:       eventEmitter(events),
		UsePublicDERP: true,
	})
	if err != nil {
		return nil, err
	}
	return &listener{inner: inner}, nil
}

// Dial validates and claims a public DERP Attach token.
func Dial(ctx context.Context, encoded string, events PathEvent) (net.Conn, error) {
	if err := ValidatePublicRoute(); err != nil {
		return nil, err
	}
	if err := ValidateToken(encoded, time.Now()); err != nil {
		return nil, err
	}
	return session.DialAttach(ctx, session.AttachDialConfig{
		Token:         encoded,
		Emitter:       eventEmitter(events),
		UsePublicDERP: true,
	})
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
// experimental mode intentionally uses only the public Tailscale DERP map.
func ValidatePublicRoute() error {
	if value, ok := os.LookupEnv(derpbind.CustomDERPServerEnv); ok && value != "" {
		return ErrCustomRoute
	}
	return nil
}

func eventEmitter(events PathEvent) *telemetry.Emitter {
	if events == nil {
		return nil
	}
	return telemetry.WithStatusHook(nil, events)
}
