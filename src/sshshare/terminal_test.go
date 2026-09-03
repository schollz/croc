//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryPTY struct {
	reads  chan []byte
	writes bytes.Buffer
	mu     sync.Mutex
	closed chan struct{}
	once   sync.Once
}

func newMemoryPTY() *memoryPTY {
	return &memoryPTY{reads: make(chan []byte, 8), closed: make(chan struct{})}
}

func (p *memoryPTY) Read(b []byte) (int, error) {
	select {
	case chunk := <-p.reads:
		return copy(b, chunk), nil
	case <-p.closed:
		return 0, io.EOF
	}
}

func (p *memoryPTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes.Write(b)
}

func (p *memoryPTY) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *memoryPTY) written() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes.String()
}

func TestTerminalHubReadOnlyAndReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pty := newMemoryPTY()
	hub := newTerminalHub(ctx, pty, nil, nil)
	t.Cleanup(func() { _ = hub.Close() })

	firstOut := new(lockedBuffer)
	firstInputR, firstInputW := io.Pipe()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- hub.Attach(ctx, firstInputR, firstOut, true, WindowSize{80, 24}, nil)
	}()
	pty.reads <- []byte("hello\r\n")
	require.Eventually(t, func() bool { return firstOut.String() == "hello\r\n" }, time.Second, time.Millisecond)
	_, err := firstInputW.Write([]byte("rw-input"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return pty.written() == "rw-input" }, time.Second, time.Millisecond)
	require.NoError(t, firstInputW.Close())
	require.NoError(t, <-firstDone)

	readOnlyOut := new(lockedBuffer)
	roR, roW := io.Pipe()
	roDone := make(chan error, 1)
	go func() {
		roDone <- hub.Attach(ctx, roR, readOnlyOut, false, WindowSize{80, 24}, nil)
	}()
	require.Eventually(t, func() bool { return readOnlyOut.String() == "hello\r\n" }, time.Second, time.Millisecond)
	_, err = roW.Write([]byte("must-not-reach-pty"))
	require.NoError(t, err)
	require.NoError(t, roW.Close())
	require.NoError(t, <-roDone)
	require.Equal(t, "rw-input", pty.written())
}

func TestTerminalHubUsesSmallestAttachedWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pty := newMemoryPTY()
	var mu sync.Mutex
	var sizes []WindowSize
	hub := newTerminalHub(ctx, pty, func(size WindowSize) error {
		mu.Lock()
		sizes = append(sizes, size)
		mu.Unlock()
		return nil
	}, nil)
	t.Cleanup(func() { _ = hub.Close() })

	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	go hub.Attach(ctx, r1, io.Discard, false, WindowSize{120, 40}, nil)
	go hub.Attach(ctx, r2, io.Discard, false, WindowSize{80, 24}, nil)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sizes) >= 2 && sizes[len(sizes)-1] == (WindowSize{80, 24})
	}, time.Second, time.Millisecond)
	require.NoError(t, w1.Close())
	require.NoError(t, w2.Close())
}
