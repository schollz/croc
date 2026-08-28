package croc

import (
	"context"
	"errors"
	"net"
	"sync"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/tailcattransport"
)

// tailcatDataTransport keeps Tailcat construction behind a client-owned seam.
// The public transport spelling remains "derp" for compatibility.
type tailcatDataTransport interface {
	Available() bool
	Listen(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error)
	Dial(context.Context, string, []byte, tailcattransport.PathEvent) (*tailcatDataBundle, error)
	ValidateOffer(string, []byte) error
}

type productionTailcatDataTransport struct {
	config tailcattransport.Config
}

func defaultTailcatDataTransport() tailcatDataTransport {
	return productionTailcatDataTransport{config: tailcatBuildConfig()}
}

func (productionTailcatDataTransport) Available() bool { return tailcattransport.Available() }

func (p productionTailcatDataTransport) Listen(ctx context.Context, sessionKey []byte, events tailcattransport.PathEvent) (tailcatDataListener, error) {
	listener, err := tailcattransport.Listen(ctx, sessionKey, p.config, events)
	if err != nil {
		return nil, err
	}
	return productionTailcatListener{Listener: listener}, nil
}

func (p productionTailcatDataTransport) Dial(ctx context.Context, offer string, sessionKey []byte, events tailcattransport.PathEvent) (*tailcatDataBundle, error) {
	bundle, err := tailcattransport.Dial(ctx, offer, sessionKey, p.config, events)
	if err != nil {
		return nil, err
	}
	return newTailcatBundle(bundle), nil
}

func (productionTailcatDataTransport) ValidateOffer(offer string, sessionKey []byte) error {
	return tailcattransport.ValidateOffer(offer, sessionKey)
}

func (c *Client) dataTransport() tailcatDataTransport {
	if c != nil && c.tailcat.transport != nil {
		return c.tailcat.transport
	}
	return defaultTailcatDataTransport()
}

type tailcatDataBundle struct {
	connections []net.Conn
	stats       func() tailcattransport.BundleStats
	cleanup     func() error
	closeOnce   sync.Once
	closeErr    error
}

func newTailcatBundle(bundle *tailcattransport.Bundle) *tailcatDataBundle {
	if bundle == nil {
		return nil
	}
	return &tailcatDataBundle{
		connections: bundle.Connections(),
		stats:       bundle.Stats,
		cleanup:     bundle.Close,
	}
}

func (b *tailcatDataBundle) addCleanup(cleanup func()) {
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

func (b *tailcatDataBundle) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		stats := tailcattransport.BundleStats{}
		if b.stats != nil {
			stats = b.stats()
		}
		if b.cleanup != nil {
			b.closeErr = b.cleanup()
		}
		log.Debugf("Tailcat transport summary: path=%s streams=%d setup=%s sent=%d received=%d",
			stats.Path, stats.StreamCount, stats.SetupDuration, stats.BytesSent, stats.BytesReceived)
	})
	return b.closeErr
}

type tailcatDataListener interface {
	Offer() string
	Accept(context.Context) (*tailcatDataBundle, error)
	Close() error
}

type productionTailcatListener struct {
	*tailcattransport.Listener
}

func (l productionTailcatListener) Accept(ctx context.Context) (*tailcatDataBundle, error) {
	bundle, err := l.Listener.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return newTailcatBundle(bundle), nil
}

func validateTailcatBundle(bundle *tailcatDataBundle) error {
	if bundle == nil || len(bundle.connections) == 0 {
		return errors.New("Tailcat returned an empty connection bundle")
	}
	for _, raw := range bundle.connections {
		if raw == nil {
			return errors.New("Tailcat returned an empty connection")
		}
	}
	return nil
}
