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

// WindowSize is a terminal size in character cells.
type WindowSize struct {
	Width  int
	Height int
}

type attachment struct {
	id       uint64
	output   io.Writer
	queue    chan []byte
	done     chan struct{}
	doneOnce sync.Once
	size     WindowSize
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
	pty         io.ReadWriteCloser
	attachments map[uint64]*attachment
	nextID      uint64
	transcript  []byte
	truncated   bool
	done        chan struct{}
	doneOnce    sync.Once
	closeOnce   sync.Once
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
			h.finish(normalizePTYReadError(err))
			return
		}
	}
}

func (h *terminalHub) broadcast(data []byte) {
	chunk := append([]byte(nil), data...)
	h.mu.Lock()
	if len(chunk) >= transcriptLimit {
		h.transcript = append(h.transcript[:0], chunk[len(chunk)-transcriptLimit:]...)
		h.truncated = true
	} else {
		if excess := len(h.transcript) + len(chunk) - transcriptLimit; excess > 0 {
			copy(h.transcript, h.transcript[excess:])
			h.transcript = h.transcript[:len(h.transcript)-excess]
			h.truncated = true
		}
		h.transcript = append(h.transcript, chunk...)
	}
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
		output: output,
		queue:  make(chan []byte, attachmentQueueChunks),
		done:   make(chan struct{}),
		size:   normalizeWindowSize(initial),
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
	replay := append([]byte(nil), h.transcript...)
	truncated := h.truncated
	newSize := h.sharedWindowSizeLocked()
	h.mu.Unlock()
	h.applyWindowSize(newSize)

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
	newSize = h.sharedWindowSizeLocked()
	h.mu.Unlock()
	h.applyWindowSize(newSize)
	return err
}

func (h *terminalHub) updateWindowSize(id uint64, size WindowSize) {
	h.mu.Lock()
	if client := h.attachments[id]; client != nil {
		client.size = normalizeWindowSize(size)
	}
	shared := h.sharedWindowSizeLocked()
	h.mu.Unlock()
	h.applyWindowSize(shared)
}

func normalizeWindowSize(size WindowSize) WindowSize {
	if size.Width <= 0 {
		size.Width = 80
	}
	if size.Height <= 0 {
		size.Height = 24
	}
	return size
}

func (h *terminalHub) sharedWindowSizeLocked() WindowSize {
	result := WindowSize{}
	for _, client := range h.attachments {
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

func (h *terminalHub) applyWindowSize(size WindowSize) {
	if h.resizePTY != nil {
		_ = h.resizePTY(normalizeWindowSize(size))
	}
}

func (h *terminalHub) Done() <-chan struct{} { return h.done }

func (h *terminalHub) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (h *terminalHub) Close() error {
	var err error
	h.closeOnce.Do(func() {
		if h.stopPTY != nil {
			err = h.stopPTY()
		}
		if closeErr := h.pty.Close(); err == nil {
			err = closeErr
		}
		h.finish(err)
	})
	return err
}
