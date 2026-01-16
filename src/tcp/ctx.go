// ctx.go
package tcp

import (
	"context"
	"errors"
	"net"
	"sync"

	log "github.com/schollz/logger"
)

// stop manages graceful shutdown of the TCP server
type stop struct {
	ctx    context.Context
	cancel context.CancelFunc
	// Track connections
	server net.Listener
	wg     sync.WaitGroup
	gui    bool
}

// newStop creates a new stop manager
func newStop(ctx context.Context) *stop {
	s := &stop{}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)

	return s
}

// Cancel initiate graceful shutdown
func (s *stop) Cancel() {
	log.Trace("tcp Cancel")
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func RunCtx(ctx context.Context, debugLevel, host, port, password string, banner ...string) error {
	return RunWithOptionsAsync(host, port, password, WithBanner(banner...), WithLogLevel(debugLevel), WithCtx(ctx))
}

func WithCtx(ctx context.Context) serverOptsFunc {
	return func(s *server) error {
		if s.stop.cancel != nil {
			s.stop.cancel()
		}
		s.stop = newStop(ctx)
		s.stop.gui = true
		return nil
	}
}

// Ignore context cancellation error
func Ignore(err error) error {
	if err != nil && (errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		// ignore Listener closed during cancellation
		// strings.Contains(err.Error(), "use of closed network connection") ||
		errors.Is(err, net.ErrClosed)) {
		log.Tracef("ignored: %v", err)
		return nil
	}
	return err
}
