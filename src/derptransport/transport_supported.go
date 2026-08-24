//go:build !linux || (!386 && !arm)

package derptransport

import (
	"context"
	"net"
	"time"

	"github.com/shayne/derphole/pkg/session"
	"github.com/shayne/derphole/pkg/telemetry"
)

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

// Available reports whether the derphole session implementation is compiled in.
func Available() bool { return true }

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

func eventEmitter(events PathEvent) *telemetry.Emitter {
	if events == nil {
		return nil
	}
	return telemetry.WithStatusHook(nil, events)
}
