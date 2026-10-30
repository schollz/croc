package croc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallelPreservesForeignSidecar(t *testing.T) {
	sourceDir := t.TempDir()
	data := bytes.Repeat([]byte("sidecar"), 50000)
	path := filepath.Join(sourceDir, "foreign.bin")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	relay := startTestRelay(t, 4)
	testDir := t.TempDir()
	receiveDir := filepath.Join(testDir, "receive")
	require.NoError(t, os.MkdirAll(receiveDir, 0o755))
	originalCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(receiveDir))
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })
	require.NoError(t, os.WriteFile(filepath.Join(receiveDir, "foreign.bin.crocstate"), []byte("user data"), 0o600))

	filesInfo, emptyFolders, folders, err := GetFilesInfo([]string{path}, false, false, nil)
	require.NoError(t, err)
	secret := fmt.Sprintf("sidecar-%d", time.Now().UnixNano())
	sender, err := newTestClient(Options{
		IsSender: true, SharedSecret: secret, RelayAddress: relay.address,
		RelayPorts: append([]string(nil), relay.allPorts...), RelayPassword: "pass123",
		NoPrompt: true, DisableLocal: true, Overwrite: true, Curve: "siec", ParallelData: "force",
	})
	require.NoError(t, err)
	receiver, err := newTestClient(Options{
		IsSender: false, SharedSecret: secret, RelayAddress: relay.address,
		RelayPassword: "pass123", NoPrompt: true, DisableLocal: true,
		Overwrite: true, Curve: "siec", ParallelData: "force",
	})
	require.NoError(t, err)
	errCh := make(chan error, 2)
	go func() { errCh <- sender.Send(filesInfo, emptyFolders, folders) }()
	time.Sleep(100 * time.Millisecond)
	go func() { errCh <- receiver.Receive() }()
	for range 2 {
		require.NoError(t, <-errCh)
	}
	got, err := os.ReadFile(filepath.Join(receiveDir, "foreign.bin"))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
	sidecar, err := os.ReadFile(filepath.Join(receiveDir, "foreign.bin.crocstate"))
	require.NoError(t, err)
	assert.Equal(t, "user data", string(sidecar))
}
