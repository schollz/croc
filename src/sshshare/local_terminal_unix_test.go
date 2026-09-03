//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func TestAttachLocalTerminalControlCStopsHost(t *testing.T) {
	terminalMaster, terminalSlave, err := pty.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = terminalMaster.Close() })
	t.Cleanup(func() { _ = terminalSlave.Close() })

	// Put the test terminal in raw mode before the attachment starts. This
	// reproduces the macOS behavior where Ctrl-C is read as 0x03 rather than
	// delivered to the process as SIGINT.
	originalState, err := term.MakeRaw(int(terminalSlave.Fd()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = term.Restore(int(terminalSlave.Fd()), originalState) })

	ctx, cancel := context.WithCancel(t.Context())
	sharedPTY := newMemoryPTY()
	hub := newTerminalHub(ctx, sharedPTY, nil, nil)
	host := &Host{ctx: ctx, cancel: cancel, hub: hub, done: make(chan struct{})}

	result := make(chan error, 1)
	go func() {
		result <- host.AttachLocalTerminal(ctx, terminalSlave, io.Discard)
	}()
	n, err := terminalMaster.Write([]byte{0x03})
	require.NoError(t, err)
	require.Equal(t, 1, n)

	select {
	case err = <-result:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("local terminal did not stop after Ctrl-C")
	}
	select {
	case <-host.Done():
	default:
		t.Fatal("host remained active after Ctrl-C")
	}
	require.Empty(t, sharedPTY.written(), "Ctrl-C reached the shared PTY")
}
