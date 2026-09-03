//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTerminalExitStatusComesFromProcessWait(t *testing.T) {
	hub, err := startTerminal(context.Background(), []string{"/bin/sh", "-c", "printf output; exit 7"}, "", WindowSize{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Close() })

	select {
	case <-hub.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("terminal process did not exit")
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, hub.Err(), &exitErr)
	require.Equal(t, 7, exitErr.ExitCode())
	closeErr := hub.Close()
	require.ErrorAs(t, closeErr, &exitErr)
	require.Equal(t, closeErr, hub.Close(), "Close should preserve the process result")
}

func TestTerminalCloseReapsProcess(t *testing.T) {
	hub, err := startTerminal(context.Background(), []string{"/bin/sh", "-c", "sleep 30"}, "", WindowSize{})
	require.NoError(t, err)
	require.NoError(t, hub.Close())
	select {
	case <-hub.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("terminal process was not reaped")
	}
	require.NoError(t, hub.Err())
	require.NoError(t, hub.Close(), "Close should return a stable result")
}
