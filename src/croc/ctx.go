// ctx.go
package croc

import (
	"context"
	"net"
	"sync"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/utils"
)

// stop manages graceful shutdown
type stop struct {
	ctx        context.Context
	cancel     context.CancelFunc
	cancelOnce sync.Once
	doneOnce   sync.Once
	stopChan   chan struct{} //peerdiscovery
	run        func(ctx context.Context, debugLevel string, listener net.Listener, password string, banner ...string) error
	hash       func(fname string, algorithm string, showProgress ...bool) (hash256 []byte, err error)
	gui        bool
}

// newStop creates a new stop manager instance
func newStop(ctx context.Context) *stop {
	s := &stop{
		stopChan: make(chan struct{}),
		run: func(ctx context.Context, debugLevel string, listener net.Listener, password string, banner ...string) error {
			return tcp.RunWithListener(listener, password, tcp.WithCtx(ctx), tcp.WithLogLevel(debugLevel), tcp.WithBanner(banner...))
		},
		hash: utils.HashFile,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)

	return s
}

func (c *Client) clientContext() context.Context {
	if c != nil && c.stop != nil && c.stop.ctx != nil {
		return c.stop.ctx
	}
	return context.Background()
}

func (s *stop) done() {
	<-s.ctx.Done()
	s.doneOnce.Do(func() {
		time.Sleep(time.Millisecond)
		close(s.stopChan)
		log.Trace("croc done")
	})
}

// NewCtx creates a client with context support
func NewCtx(ctx context.Context, ops Options) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Create a regular c
	c, err := New(ops)
	if err != nil {
		return nil, err
	}
	c.stop = newStop(ctx)
	c.stop.gui = true
	c.stop.hash = func(fname string, algorithm string, showProgress ...bool) (hash256 []byte, err error) {
		return utils.HashFileCtx(c.stop.ctx, fname, algorithm, showProgress...)
	}

	go func() {
		select {
		case <-ctx.Done():
			log.Trace("parent context canceled")
			// Notify the peer when possible, but never let a blocked best-effort
			// notification delay local shutdown. Closing every active connection
			// then wakes reads and writes that cannot select on the context.
			notified := make(chan struct{})
			go func() {
				c.SendError()
				close(notified)
			}()
			timer := time.NewTimer(25 * time.Millisecond)
			select {
			case <-notified:
				timer.Stop()
			case <-timer.C:
			}
			c.closeAttempt()
		case <-c.stopChan:
			// for stop goroutine
		}
		log.Trace("croc NewCtx done")
	}()

	return c, nil
}

// ctxErr checks whether it is necessary to interrupt my loops and goroutines
func (s *stop) ctxErr() error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return nil
	}
}

// Cancel initiates interruption of my loops and goroutines
func (s *stop) Cancel() {
	s.cancelOnce.Do(func() {
		log.Trace("croc Cancel")
		s.cancel()
	})
}

// SendError tells the peer to interrupt their loops and goroutines
func (c *Client) SendError() {
	control := c.connection(0)
	if c.Key != nil && control != nil {
		message.Send(control, c.Key, message.Message{
			Type:    message.TypeError,
			Message: "refusing files",
		})
		time.Sleep(time.Millisecond)
	}
}
