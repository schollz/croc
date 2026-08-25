//go:build !linux || (!386 && !arm)

package derptransport

import (
	"context"
	"errors"
	"net"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/shayne/derphole/pkg/session"
	"github.com/shayne/derphole/pkg/telemetry"
	"github.com/shayne/derphole/pkg/transport"
)

type listener struct {
	inner *session.AttachListener
}

type groupListener struct {
	inner *session.AttachGroupListener
}

type bundle struct {
	inner *session.AttachGroup
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

func (l *groupListener) Token() string {
	if l == nil || l.inner == nil {
		return ""
	}
	return l.inner.Token
}

func (l *groupListener) Accept(ctx context.Context) (Bundle, error) {
	if l == nil || l.inner == nil {
		return nil, net.ErrClosed
	}
	group, err := l.inner.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return &bundle{inner: group}, nil
}

func (l *groupListener) Close() error {
	if l == nil || l.inner == nil {
		return net.ErrClosed
	}
	return l.inner.Close()
}

func (b *bundle) Connections() []net.Conn {
	if b == nil || b.inner == nil {
		return nil
	}
	return b.inner.Connections()
}

func (b *bundle) Stats() BundleStats {
	if b == nil || b.inner == nil {
		return BundleStats{}
	}
	stats := b.inner.Stats()
	return BundleStats{
		Mode:              string(stats.Mode),
		Path:              pathName(stats.Path),
		StreamCount:       stats.StreamCount,
		RawPathCount:      stats.RawPathCount,
		SetupDuration:     stats.SetupDuration,
		RawSetupDuration:  stats.RawSetupDuration,
		FallbackDuration:  stats.FallbackDuration,
		FallbackReason:    stats.FallbackReason,
		CandidateDuration: stats.Phases.CandidateGatherDuration,
		ExchangeDuration:  stats.Phases.CandidateExchangeDuration,
		PunchDuration:     stats.Phases.PunchDuration,
		SelectionDuration: stats.Phases.SelectionDuration,
		HandshakeDuration: stats.Phases.HandshakeDuration,
		ReadinessDuration: stats.Phases.ReadinessDuration,
		BytesSent:         stats.Dataplane.BytesSent,
		BytesReceived:     stats.Dataplane.BytesReceived,
		PacketsSent:       stats.Dataplane.PacketsSent,
		PacketsReceived:   stats.Dataplane.PacketsReceived,
		PacketsLost:       stats.Dataplane.PacketsLost,
		WireBytesSent:     stats.Dataplane.WireBytesSent,
		RecoveryWireBytes: stats.Dataplane.RecoveryWireBytes,
		SmoothedRTT:       stats.Dataplane.SmoothedRTT,
	}
}

func pathName(path transport.Path) string {
	switch path {
	case transport.PathRelay:
		return "relay"
	case transport.PathDirect:
		return "direct"
	default:
		return "unknown"
	}
}

func (b *bundle) Close() error {
	if b == nil || b.inner == nil {
		return net.ErrClosed
	}
	return b.inner.Close()
}

// Available reports whether the derphole session implementation is compiled in.
func Available() bool { return true }

// IsCleanGroupClose recognizes derphole's terminal AttachGroup close without
// leaking QUIC-specific error details into croc's transfer state machine.
func IsCleanGroupClose(err error) bool {
	var appErr *quic.ApplicationError
	return errors.As(err, &appErr) && appErr.ErrorCode == 0 && appErr.ErrorMessage == "attach group complete"
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

// ListenGroup creates a public AttachGroup listener optimized for striped
// file-transfer consumers.
func ListenGroup(ctx context.Context, events PathEvent) (GroupListener, error) {
	return ListenGroupWithConfig(ctx, events, DefaultGroupConfig())
}

func ListenGroupWithConfig(ctx context.Context, events PathEvent, cfg GroupConfig) (GroupListener, error) {
	cfg = normalizeGroupConfig(cfg)
	if err := ValidatePublicRoute(); err != nil {
		return nil, err
	}
	inner, err := session.ListenAttachGroup(ctx, session.AttachGroupListenConfig{
		Emitter:         eventEmitter(events),
		ForceRelay:      cfg.ForceRelay,
		UsePublicDERP:   true,
		MaxStreams:      cfg.StreamCount,
		MaxRawPaths:     cfg.MaxRawPaths,
		RawDirectBudget: cfg.RawDirectBudget,
	})
	if err != nil {
		return nil, err
	}
	return &groupListener{inner: inner}, nil
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

// DialGroup validates and claims a public AttachGroup token.
func DialGroup(ctx context.Context, encoded string, events PathEvent) (Bundle, error) {
	return DialGroupWithConfig(ctx, encoded, events, DefaultGroupConfig())
}

func DialGroupWithConfig(ctx context.Context, encoded string, events PathEvent, cfg GroupConfig) (Bundle, error) {
	cfg = normalizeGroupConfig(cfg)
	if err := ValidatePublicRoute(); err != nil {
		return nil, err
	}
	if err := ValidateGroupToken(encoded, time.Now()); err != nil {
		return nil, err
	}
	group, err := session.DialAttachGroup(ctx, session.AttachGroupDialConfig{
		Token:         encoded,
		Emitter:       eventEmitter(events),
		ForceRelay:    cfg.ForceRelay,
		UsePublicDERP: true,
		StreamCount:   cfg.StreamCount,
	})
	if err != nil {
		return nil, err
	}
	return &bundle{inner: group}, nil
}

func eventEmitter(events PathEvent) *telemetry.Emitter {
	if events == nil {
		return nil
	}
	return telemetry.WithStatusHook(nil, events)
}
