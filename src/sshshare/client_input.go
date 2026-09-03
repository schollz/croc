//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"io"
	"sync"
)

const pendingInputLimit = 64 << 10

// inputBroker keeps exactly one goroutine reading the local terminal across
// reconnects. Each SSH connection gets a short-lived reader; retiring it
// prevents a stale ssh.Session copy goroutine from stealing keystrokes from a
// later connection.
type inputBroker struct {
	mu       sync.Mutex
	start    sync.Once
	source   io.Reader
	current  *brokerReader
	pending  []byte
	closed   bool
	detached bool
}

type brokerReader struct {
	mu        sync.Mutex
	chunks    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	buffer    []byte
}

func newInputBroker(source io.Reader) *inputBroker {
	return &inputBroker{source: source}
}

func (b *inputBroker) activate() *brokerReader {
	b.start.Do(func() { go b.pump() })
	r := &brokerReader{chunks: make(chan []byte, 64), done: make(chan struct{})}
	b.mu.Lock()
	old := b.current
	b.current = r
	r.buffer = append(r.buffer, b.pending...)
	b.pending = nil
	closed := b.closed
	b.mu.Unlock()
	if old != nil {
		old.close()
	}
	if closed {
		r.close()
	}
	return r
}

func (b *inputBroker) deactivate(r *brokerReader) {
	b.mu.Lock()
	if b.current == r {
		b.current = nil
	}
	b.mu.Unlock()
	r.close()
}

func (b *inputBroker) pump() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := b.source.Read(buffer)
		if n > 0 {
			data := buffer[:n]
			if before, _, ok := bytes.Cut(data, []byte{0x1d}); ok { // Ctrl-]
				b.deliver(before)
				b.finish(true)
				return
			}
			b.deliver(data)
		}
		if err != nil {
			b.finish(false)
			return
		}
	}
}

func (b *inputBroker) deliver(data []byte) {
	if len(data) == 0 {
		return
	}
	chunk := append([]byte(nil), data...)
	for {
		b.mu.Lock()
		current := b.current
		if current == nil {
			remaining := pendingInputLimit - len(b.pending)
			if remaining > 0 {
				if len(chunk) > remaining {
					chunk = chunk[:remaining]
				}
				b.pending = append(b.pending, chunk...)
			}
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
		select {
		case current.chunks <- chunk:
			return
		case <-current.done:
			// Retry against the replacement connection, or queue the bytes if
			// there is currently no active connection.
		}
	}
}

func (b *inputBroker) finish(detached bool) {
	b.mu.Lock()
	b.closed = true
	b.detached = detached
	current := b.current
	b.current = nil
	b.mu.Unlock()
	if current != nil {
		current.close()
	}
}

func (b *inputBroker) Detached() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.detached
}

func (r *brokerReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.buffer) == 0 {
		select {
		case chunk := <-r.chunks:
			r.buffer = chunk
		default:
			select {
			case chunk := <-r.chunks:
				r.buffer = chunk
			case <-r.done:
				// Drain chunks queued before EOF/detach before reporting EOF.
				select {
				case chunk := <-r.chunks:
					r.buffer = chunk
				default:
					return 0, io.EOF
				}
			}
		}
	}
	n := copy(buffer, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

func (r *brokerReader) close() {
	r.closeOnce.Do(func() { close(r.done) })
}
