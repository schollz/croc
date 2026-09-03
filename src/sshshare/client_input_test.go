//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputBrokerMovesInputToReconnectedSession(t *testing.T) {
	source, writer := io.Pipe()
	t.Cleanup(func() { _ = source.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	broker := newInputBroker(source)

	first := broker.activate()
	_, err := writer.Write([]byte("first"))
	require.NoError(t, err)
	got := make([]byte, 5)
	_, err = io.ReadFull(first, got)
	require.NoError(t, err)
	require.Equal(t, "first", string(got))
	broker.deactivate(first)

	_, err = writer.Write([]byte("second"))
	require.NoError(t, err)
	second := broker.activate()
	got = make([]byte, 6)
	_, err = io.ReadFull(second, got)
	require.NoError(t, err)
	require.Equal(t, "second", string(got))
	broker.deactivate(second)
}

func TestInputBrokerDetachesAndPreservesPrefix(t *testing.T) {
	source, writer := io.Pipe()
	t.Cleanup(func() { _ = source.Close() })
	broker := newInputBroker(source)
	reader := broker.activate()

	_, err := writer.Write([]byte("keep\x1ddrop"))
	require.NoError(t, err)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "keep", string(got))
	require.True(t, broker.Detached())
}

func TestInputBrokerPassesControlCToSharedShell(t *testing.T) {
	broker := newInputBroker(bytes.NewBuffer([]byte("before\x03after")))
	reader := broker.activate()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("before\x03after"), got)
	require.False(t, broker.Detached())
}
