//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
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
	ctx := t.Context()
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
	ctx := t.Context()
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
	go hub.Attach(ctx, r1, io.Discard, true, WindowSize{120, 40}, nil)
	go hub.Attach(ctx, r2, io.Discard, true, WindowSize{80, 24}, nil)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sizes) >= 2 && sizes[len(sizes)-1] == (WindowSize{80, 24})
	}, time.Second, time.Millisecond)
	require.NoError(t, w1.Close())
	require.NoError(t, w2.Close())
}

func TestTerminalHubIgnoresReadOnlyWindowChanges(t *testing.T) {
	ctx := t.Context()
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

	rwR, rwW := io.Pipe()
	roR, roW := io.Pipe()
	resizes := make(chan WindowSize, 1)
	go hub.Attach(ctx, rwR, io.Discard, true, WindowSize{120, 40}, nil)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sizes) > 0 && sizes[len(sizes)-1] == (WindowSize{120, 40})
	}, time.Second, time.Millisecond)
	go hub.Attach(ctx, roR, io.Discard, false, WindowSize{20, 5}, resizes)
	resizes <- WindowSize{10, 2}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sizes) >= 3 && sizes[len(sizes)-1] == (WindowSize{120, 40})
	}, time.Second, time.Millisecond)
	require.NoError(t, rwW.Close())
	require.NoError(t, roW.Close())
}

func TestByteRingKeepsNewestBytesInOrder(t *testing.T) {
	ring := newByteRing(8)
	ring.Write([]byte("abc"))
	require.Equal(t, "abc", string(ring.Bytes()))
	require.False(t, ring.Truncated())

	ring.Write([]byte("defghi"))
	require.Equal(t, "bcdefghi", string(ring.Bytes()))
	require.True(t, ring.Truncated())

	ring.Write([]byte("0123456789"))
	require.Equal(t, "23456789", string(ring.Bytes()))
}

func TestNormalizeWindowSizeBoundsPTYDimensions(t *testing.T) {
	require.Equal(t, WindowSize{Width: 80, Height: 24}, normalizeWindowSize(WindowSize{}))
	require.Equal(t, WindowSize{Width: 65535, Height: 65535}, normalizeWindowSize(WindowSize{Width: 1 << 20, Height: 1 << 20}))
}
