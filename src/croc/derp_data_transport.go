package croc

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/derptransport"
	log "github.com/schollz/logger"
)

// derpDataTransport keeps derphole-specific construction behind a small,
// client-owned seam. Production clients use the immutable implementation
// below; tests can inject a provider without mutating package globals.
type derpDataTransport interface {
	Available() bool
	AttachGroupEnabled() bool
	Listen(context.Context, derptransport.PathEvent, bool) (derpDataListener, error)
	Dial(context.Context, string, derptransport.PathEvent, bool) (*derpDataBundle, error)
	ValidateToken(string, time.Time, bool) error
}

type productionDERPDataTransport struct {
	attachGroupEnabled bool
	groupConfig        derptransport.GroupConfig
}

func defaultDERPDataTransport() derpDataTransport {
	return productionDERPDataTransport{
		attachGroupEnabled: derpAttachGroupBuildEnabled(),
		groupConfig:        derpAttachGroupBuildConfig(),
	}
}

func (productionDERPDataTransport) Available() bool { return derptransport.Available() }

func (p productionDERPDataTransport) AttachGroupEnabled() bool {
	return p.attachGroupEnabled
}

func (p productionDERPDataTransport) Listen(ctx context.Context, events derptransport.PathEvent, grouped bool) (derpDataListener, error) {
	if grouped {
		listener, err := derptransport.ListenGroupWithConfig(ctx, events, p.groupConfig)
		if err != nil {
			return nil, err
		}
		return groupDERPDataListener{GroupListener: listener}, nil
	}
	listener, err := derptransport.Listen(ctx, events)
	if err != nil {
		return nil, err
	}
	return singleDERPDataListener{Listener: listener}, nil
}

func (p productionDERPDataTransport) Dial(ctx context.Context, tokenValue string, events derptransport.PathEvent, grouped bool) (*derpDataBundle, error) {
	if grouped {
		bundle, err := derptransport.DialGroupWithConfig(ctx, tokenValue, events, p.groupConfig)
		if err != nil {
			return nil, err
		}
		return newGroupDERPBundle(bundle), nil
	}
	conn, err := derptransport.Dial(ctx, tokenValue, events)
	if err != nil {
		return nil, err
	}
	return newSingleDERPBundle(conn), nil
}

func (productionDERPDataTransport) ValidateToken(tokenValue string, now time.Time, grouped bool) error {
	if grouped {
		return derptransport.ValidateGroupToken(tokenValue, now)
	}
	return derptransport.ValidateToken(tokenValue, now)
}

func (c *Client) dataTransport() derpDataTransport {
	if c != nil && c.derpTransport != nil {
		return c.derpTransport
	}
	return defaultDERPDataTransport()
}

type derpDataBundle struct {
	connections []net.Conn
	stats       func() derptransport.BundleStats
	cleanup     func() error
	closeOnce   sync.Once
	closeErr    error
}

func newSingleDERPBundle(conn net.Conn) *derpDataBundle {
	if conn == nil {
		return nil
	}
	return &derpDataBundle{
		connections: []net.Conn{conn},
		stats: func() derptransport.BundleStats {
			return derptransport.BundleStats{Mode: "legacy", StreamCount: 1}
		},
		cleanup: conn.Close,
	}
}

func newGroupDERPBundle(bundle derptransport.Bundle) *derpDataBundle {
	if bundle == nil {
		return nil
	}
	return &derpDataBundle{
		connections: append([]net.Conn(nil), bundle.Connections()...),
		stats:       bundle.Stats,
		cleanup:     bundle.Close,
	}
}

func (b *derpDataBundle) addCleanup(cleanup func()) {
	if b == nil || cleanup == nil {
		return
	}
	previous := b.cleanup
	b.cleanup = func() error {
		var err error
		if previous != nil {
			err = previous()
		}
		cleanup()
		return err
	}
}

func (b *derpDataBundle) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		stats := derptransport.BundleStats{}
		if b.stats != nil {
			stats = b.stats()
		}
		log.Debugf("DERP transport summary: mode=%s path=%s streams=%d raw_paths=%d sent=%d received=%d packets_sent=%d packets_received=%d packets_lost=%d wire_sent=%d recovery_wire=%d rtt=%s setup=%s raw_setup=%s candidates=%s exchange=%s punch=%s selection=%s handshake=%s readiness=%s fallback_duration=%s fallback=%s",
			stats.Mode, stats.Path, stats.StreamCount, stats.RawPathCount, stats.BytesSent, stats.BytesReceived,
			stats.PacketsSent, stats.PacketsReceived, stats.PacketsLost, stats.WireBytesSent,
			stats.RecoveryWireBytes, stats.SmoothedRTT, stats.SetupDuration, stats.RawSetupDuration,
			stats.CandidateDuration, stats.ExchangeDuration, stats.PunchDuration, stats.SelectionDuration,
			stats.HandshakeDuration, stats.ReadinessDuration, stats.FallbackDuration, stats.FallbackReason)
		if b.cleanup != nil {
			b.closeErr = b.cleanup()
		}
	})
	return b.closeErr
}

type derpDataListener interface {
	Token() string
	Accept(context.Context) (*derpDataBundle, error)
	Close() error
}

type singleDERPDataListener struct{ derptransport.Listener }

func (l singleDERPDataListener) Accept(ctx context.Context) (*derpDataBundle, error) {
	conn, err := l.Listener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return newSingleDERPBundle(conn), nil
}

type groupDERPDataListener struct{ derptransport.GroupListener }

func (l groupDERPDataListener) Accept(ctx context.Context) (*derpDataBundle, error) {
	bundle, err := l.GroupListener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return newGroupDERPBundle(bundle), nil
}

func validateDERPBundle(bundle *derpDataBundle) error {
	if bundle == nil || len(bundle.connections) == 0 {
		return errors.New("DERP returned an empty connection bundle")
	}
	for _, raw := range bundle.connections {
		if raw == nil {
			return errors.New("DERP returned an empty connection")
		}
	}
	return nil
}
