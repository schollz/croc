// ctx.go
package croc

import (
	"context"
	"time"

	"github.com/schollz/croc/v10/src/message"
	"github.com/schollz/croc/v10/src/tcp"
	"github.com/schollz/croc/v10/src/utils"
	log "github.com/schollz/logger"
)

// stop manages graceful shutdown
type stop struct {
	ctx      context.Context
	cancel   context.CancelFunc
	stopChan chan struct{} //peerdiscovery
	run      func(debugLevel string, host string, port string, password string, banner ...string) (err error)
	hash     func(fname string, algorithm string, showProgress ...bool) (hash256 []byte, err error)
	gui      bool
}

// newStop creates a new stop manager instance
func newStop(ctx context.Context) *stop {
	s := &stop{
		stopChan: make(chan struct{}),
		run:      tcp.Run,
		hash:     utils.HashFile,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)

	return s
}

func (s *stop) done() {
	<-s.ctx.Done()
	time.Sleep(time.Millisecond)
	close(s.stopChan)
	log.Trace("croc done")
}

// NewCtx creates a client with context support
func NewCtx(ctx context.Context, ops Options) (*Client, error) {
	// Create a regular c
	c, err := New(ops)
	if err != nil {
		return nil, err
	}
	c.stop = newStop(ctx)
	c.stop.gui = true
	c.stop.run = func(debugLevel string, host string, port string, password string, banner ...string) (err error) {
		return tcp.RunCtx(c.stop.ctx, debugLevel, host, port, password, banner...)
	}
	c.stop.hash = func(fname string, algorithm string, showProgress ...bool) (hash256 []byte, err error) {
		return utils.HashFileCtx(c.stop.ctx, fname, algorithm, showProgress...)
	}

	go func() {
		select {
		case <-ctx.Done():
			log.Trace("parent context canceled")
			c.SendError()
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
	log.Trace("croc Cancel")
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// SendError tells the peer to interrupt their loops and goroutines
func (c *Client) SendError() {
	if c.Key != nil && len(c.conn) > 0 && c.conn[0] != nil {
		message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeError,
			Message: "refusing files",
		})
		time.Sleep(time.Millisecond)
	}
}
