//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"io"
	"sync"
)

const (
	transcriptLimit       = 8 << 20
	attachmentQueueChunks = 128
)

type attachment struct {
	id       uint64
	output   io.Writer
	queue    chan []byte
	done     chan struct{}
	doneOnce sync.Once
	size     WindowSize
	writable bool
}

func (a *attachment) stop() {
	a.doneOnce.Do(func() { close(a.done) })
}

// terminalHub owns one shell PTY and lets multiple SSH clients attach to the
// same byte stream. Writers feed the PTY; read-only clients receive identical
// output but their input is drained without reaching the shell.
type terminalHub struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	resizeMu    sync.Mutex
	pty         io.ReadWriteCloser
	attachments map[uint64]*attachment
	nextID      uint64
	transcript  byteRing
	done        chan struct{}
	doneOnce    sync.Once
	closeOnce   sync.Once
	closeErr    error
	err         error

	resizePTY func(WindowSize) error
	stopPTY   func() error
}

func newTerminalHub(
	ctx context.Context,
	pty io.ReadWriteCloser,
	resize func(WindowSize) error,
	stop func() error,
) *terminalHub {
	hubCtx, cancel := context.WithCancel(ctx)
	h := &terminalHub{
		ctx:         hubCtx,
		cancel:      cancel,
		pty:         pty,
		attachments: make(map[uint64]*attachment),
		transcript:  newByteRing(transcriptLimit),
		done:        make(chan struct{}),
		resizePTY:   resize,
		stopPTY:     stop,
	}
	go h.readOutput()
	go func() {
		<-hubCtx.Done()
		_ = h.Close()
	}()
	return h
}

func (h *terminalHub) readOutput() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := h.pty.Read(buffer)
		if n > 0 {
			h.broadcast(buffer[:n])
		}
		if err != nil {
			if normalized := normalizePTYReadError(err); normalized != nil {
				h.finish(normalized)
			}
			return
		}
	}
}

func (h *terminalHub) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	h.mu.Lock()
	h.transcript.Write(chunk)
	for _, client := range h.attachments {
		select {
		case client.queue <- chunk:
		default:
			// A stalled client must not stall the shared shell or corrupt every
			// other viewer. Disconnect it so a reconnect gets a clean replay.
			client.stop()
		}
	}
	h.mu.Unlock()
}

func (h *terminalHub) finish(err error) {
	h.doneOnce.Do(func() {
		h.mu.Lock()
		h.err = err
		for _, client := range h.attachments {
			client.stop()
		}
		h.mu.Unlock()
		close(h.done)
		h.cancel()
	})
}

// Attach adds one terminal client until its input closes, its context is
// canceled, or the shared shell exits. The recorded transcript is replayed
// before live output so reconnecting clients recover the current display.
func (h *terminalHub) Attach(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	writable bool,
	initial WindowSize,
	resizes <-chan WindowSize,
) error {
	if input == nil || output == nil {
		return errors.New("terminal attachment requires input and output")
	}
	client := &attachment{
		output:   output,
		queue:    make(chan []byte, attachmentQueueChunks),
		done:     make(chan struct{}),
		size:     normalizeWindowSize(initial),
		writable: writable,
	}
	h.mu.Lock()
	select {
	case <-h.done:
		err := h.err
		h.mu.Unlock()
		return err
	default:
	}
	h.nextID++
	client.id = h.nextID
	h.attachments[client.id] = client
	replay := h.transcript.Bytes()
	truncated := h.transcript.Truncated()
	h.mu.Unlock()
	h.applyWindowSize()

	writeDone := make(chan error, 1)
	go func() {
		if len(replay) > 0 {
			if truncated {
				_, _ = io.WriteString(output, "\x1bc\r\n[croc ssh: replay begins after older output was trimmed]\r\n")
			}
			if _, err := output.Write(replay); err != nil {
				writeDone <- err
				return
			}
		}
		for {
			select {
			case chunk := <-client.queue:
				if _, err := output.Write(chunk); err != nil {
					writeDone <- err
					return
				}
			case <-client.done:
				writeDone <- nil
				return
			case <-ctx.Done():
				writeDone <- ctx.Err()
				return
			case <-h.done:
				writeDone <- h.Err()
				return
			}
		}
	}()

	inputDone := make(chan error, 1)
	go func() {
		if writable {
			_, err := io.Copy(h.pty, input)
			inputDone <- err
			return
		}
		_, err := io.Copy(io.Discard, input)
		inputDone <- err
	}()

	if resizes != nil {
		go func() {
			for {
				select {
				case size, ok := <-resizes:
					if !ok {
						return
					}
					h.updateWindowSize(client.id, size)
				case <-client.done:
					return
				case <-ctx.Done():
					return
				case <-h.done:
					return
				}
			}
		}()
	}

	var err error
	select {
	case err = <-inputDone:
	case err = <-writeDone:
	case <-client.done:
	case <-ctx.Done():
		err = ctx.Err()
	case <-h.done:
		err = h.Err()
	}
	client.stop()
	h.mu.Lock()
	delete(h.attachments, client.id)
	h.mu.Unlock()
	h.applyWindowSize()
	return err
}

func (h *terminalHub) updateWindowSize(id uint64, size WindowSize) {
	h.mu.Lock()
	client := h.attachments[id]
	if client != nil && client.writable {
		client.size = normalizeWindowSize(size)
	}
	h.mu.Unlock()
	h.applyWindowSize()
}

func normalizeWindowSize(size WindowSize) WindowSize {
	if size.Width <= 0 {
		size.Width = 80
	}
	if size.Height <= 0 {
		size.Height = 24
	}
	if size.Width > 65535 {
		size.Width = 65535
	}
	if size.Height > 65535 {
		size.Height = 65535
	}
	return size
}

func (h *terminalHub) sharedWindowSizeLocked() WindowSize {
	result := WindowSize{}
	for _, client := range h.attachments {
		if !client.writable {
			continue
		}
		size := normalizeWindowSize(client.size)
		if result.Width == 0 || size.Width < result.Width {
			result.Width = size.Width
		}
		if result.Height == 0 || size.Height < result.Height {
			result.Height = size.Height
		}
	}
	return normalizeWindowSize(result)
}

// byteRing retains the newest bytes without copying the retained transcript
// on every append. Bytes materializes one ordered snapshot only when a client
// attaches.
type byteRing struct {
	buffer    []byte
	start     int
	length    int
	truncated bool
}

func newByteRing(capacity int) byteRing {
	if capacity < 0 {
		capacity = 0
	}
	return byteRing{buffer: make([]byte, capacity)}
}

func (r *byteRing) Write(data []byte) {
	capacity := len(r.buffer)
	if capacity == 0 {
		r.truncated = r.truncated || len(data) > 0
		return
	}
	if len(data) >= capacity {
		r.truncated = r.truncated || r.length > 0 || len(data) > capacity
		copy(r.buffer, data[len(data)-capacity:])
		r.start = 0
		r.length = capacity
		return
	}
	if overflow := r.length + len(data) - capacity; overflow > 0 {
		r.start = (r.start + overflow) % capacity
		r.length -= overflow
		r.truncated = true
	}
	end := (r.start + r.length) % capacity
	first := min(len(data), capacity-end)
	copy(r.buffer[end:], data[:first])
	copy(r.buffer, data[first:])
	r.length += len(data)
}

func (r *byteRing) Bytes() []byte {
	result := make([]byte, r.length)
	if r.length == 0 {
		return result
	}
	first := min(r.length, len(r.buffer)-r.start)
	copy(result, r.buffer[r.start:r.start+first])
	copy(result[first:], r.buffer[:r.length-first])
	return result
}

func (r *byteRing) Truncated() bool { return r.truncated }

func (h *terminalHub) applyWindowSize() {
	if h.resizePTY != nil {
		// Recompute under a separate serialization lock so concurrent attach,
		// resize, and detach calls cannot apply an older size last.
		h.resizeMu.Lock()
		defer h.resizeMu.Unlock()
		h.mu.Lock()
		size := h.sharedWindowSizeLocked()
		h.mu.Unlock()
		_ = h.resizePTY(size)
	}
}

func (h *terminalHub) Done() <-chan struct{} { return h.done }

func (h *terminalHub) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (h *terminalHub) Close() error {
	h.closeOnce.Do(func() {
		if h.stopPTY != nil {
			h.closeErr = errors.Join(h.closeErr, h.stopPTY())
		}
		h.closeErr = errors.Join(h.closeErr, h.pty.Close())
		h.finish(h.closeErr)
	})
	return h.closeErr
}
