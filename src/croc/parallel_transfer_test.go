package croc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/schollz/croc/v11/src/resume"
	"github.com/schollz/croc/v11/src/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runParallelTest(t *testing.T, senderOpts, receiverOpts Options, sources []string) (string, *Client, *Client) {
	t.Helper()
	relay := startTestRelay(t, 4)
	testDir := t.TempDir()
	receiveDir := filepath.Join(testDir, "receive")
	require.NoError(t, os.MkdirAll(receiveDir, 0o755))
	originalCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(receiveDir))
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })

	filesInfo, emptyFolders, folders, err := GetFilesInfo(sources, false, false, nil)
	require.NoError(t, err)

	secret := fmt.Sprintf("parallel-%d", time.Now().UnixNano())
	senderOpts.IsSender = true
	senderOpts.SharedSecret = secret
	senderOpts.RelayAddress = relay.address
	senderOpts.RelayPorts = append([]string(nil), relay.allPorts...)
	senderOpts.RelayPassword = "pass123"
	senderOpts.NoPrompt = true
	senderOpts.DisableLocal = true
	senderOpts.Overwrite = true
	senderOpts.Curve = "siec"
	receiverOpts.IsSender = false
	receiverOpts.SharedSecret = secret
	receiverOpts.RelayAddress = relay.address
	receiverOpts.RelayPassword = "pass123"
	receiverOpts.NoPrompt = true
	receiverOpts.DisableLocal = true
	receiverOpts.Overwrite = true
	receiverOpts.Curve = "siec"

	sender, err := newTestClient(senderOpts)
	require.NoError(t, err)
	receiver, err := newTestClient(receiverOpts)
	require.NoError(t, err)

	errCh := make(chan error, 2)
	go func() { errCh <- sender.Send(filesInfo, emptyFolders, folders) }()
	time.Sleep(100 * time.Millisecond)
	go func() { errCh <- receiver.Receive() }()
	for range 2 {
		require.NoError(t, <-errCh)
	}
	return receiveDir, sender, receiver
}

func TestParallelMultiFileBatch(t *testing.T) {
	sourceDir := t.TempDir()
	var sources []string
	want := map[string][]byte{}
	for i := 0; i < 12; i++ {
		size := 1 + i*17 + i*i*997
		data := bytes.Repeat([]byte{byte('a' + i)}, size)
		name := fmt.Sprintf("file-%02d.bin", i)
		path := filepath.Join(sourceDir, name)
		require.NoError(t, os.WriteFile(path, data, 0o644))
		sources = append(sources, path)
		want[name] = data
	}
	receiveDir, _, _ := runParallelTest(t, Options{ParallelData: "force"}, Options{ParallelData: "force"}, sources)
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(receiveDir, name))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(data, got), "content mismatch for %s", name)
		_, err = os.Stat(filepath.Join(receiveDir, name+".crocstate"))
		assert.True(t, os.IsNotExist(err), "resume journal must be removed after success")
	}
	leftovers, err := filepath.Glob(filepath.Join(receiveDir, ".*.crocpart*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "temporary files must be renamed after success")
}

func TestParallelDataOffFallsBack(t *testing.T) {
	sourceDir := t.TempDir()
	data := bytes.Repeat([]byte("legacy"), 100000)
	path := filepath.Join(sourceDir, "legacy.bin")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	receiveDir, _, _ := runParallelTest(t, Options{ParallelData: "off"}, Options{ParallelData: "off"}, []string{path})
	got, err := os.ReadFile(filepath.Join(receiveDir, "legacy.bin"))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
}

func TestParallelResumeUsesJournal(t *testing.T) {
	sourceDir := t.TempDir()
	data := bytes.Repeat([]byte("resume-data-"), 1<<17)
	path := filepath.Join(sourceDir, "resume.bin")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	relay := startTestRelay(t, 4)
	testDir := t.TempDir()
	receiveDir := filepath.Join(testDir, "receive")
	require.NoError(t, os.MkdirAll(receiveDir, 0o755))
	originalCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(receiveDir))
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })

	const chunkSize = int64(256 << 10)
	require.Zero(t, len(data)%int(chunkSize))
	partial := append([]byte(nil), data[:2*chunkSize]...)
	partial = append(partial, make([]byte, len(data)-len(partial))...)
	dest := filepath.Join(receiveDir, "resume.bin")
	require.NoError(t, os.WriteFile(dest, partial, 0o600))

	sourceHash, err := utils.HashFile(path, "xxhash", false)
	require.NoError(t, err)
	meta := resume.Metadata{
		FileSize:      int64(len(data)),
		SourceHash:    sourceHash,
		HashAlgorithm: "xxhash",
		ChunkSize:     chunkSize,
		ChunkCount:    int64(len(data)) / chunkSize,
		StateVersion:  1,
	}
	journal := resume.New(meta)
	for i := int64(0); i < 2; i++ {
		start := i * chunkSize
		end := start + chunkSize
		journal.MarkComplete(i, xxhash.Sum64(data[start:end]))
	}
	require.NoError(t, os.WriteFile(dest+".crocstate", journal.Marshal(), 0o600))

	filesInfo, emptyFolders, folders, err := GetFilesInfo([]string{path}, false, false, nil)
	require.NoError(t, err)

	secret := fmt.Sprintf("resume-%d", time.Now().UnixNano())
	sender, err := newTestClient(Options{
		IsSender: true, SharedSecret: secret, RelayAddress: relay.address,
		RelayPorts: append([]string(nil), relay.allPorts...), RelayPassword: "pass123",
		NoPrompt: true, DisableLocal: true, Overwrite: true, Curve: "siec",
		ParallelData: "force",
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
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
	_, err = os.Stat(dest + ".crocstate")
	assert.True(t, os.IsNotExist(err))
}

func TestParallelProgressiveHashBatch(t *testing.T) {
	sourceDir := t.TempDir()
	var sources []string
	want := map[string][]byte{}
	for i := 0; i < 20; i++ {
		data := bytes.Repeat([]byte{byte('A' + i%26)}, 4096+i*311)
		name := fmt.Sprintf("progressive-%02d.bin", i)
		path := filepath.Join(sourceDir, name)
		require.NoError(t, os.WriteFile(path, data, 0o644))
		sources = append(sources, path)
		want[name] = data
	}
	receiveDir, _, _ := runParallelTest(t,
		Options{ParallelData: "force", HashAlgorithm: "imohash"},
		Options{ParallelData: "force"},
		sources)
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(receiveDir, name))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(data, got), "content mismatch for %s", name)
	}
}

func TestParallelAutotuneSamples(t *testing.T) {
	sourceDir := t.TempDir()
	data := bytes.Repeat([]byte("autotune"), 1<<17)
	path := filepath.Join(sourceDir, "autotune.bin")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	_, sender, _ := runParallelTest(t,
		Options{ParallelData: "force", ThrottleUpload: "256K"},
		Options{ParallelData: "force"},
		[]string{path})
	sender.stats.mu.Lock()
	rtt := sender.stats.rtt
	chunk := sender.stats.chunk
	workers := sender.stats.workers
	sender.stats.mu.Unlock()
	assert.Greater(t, rtt, time.Duration(0), "runtime probe should measure RTT")
	assert.NotZero(t, chunk, "controller should report a chunk size")
	assert.NotZero(t, workers, "controller should report active workers")
}

func TestParallelSmallFileResumeRenamesTemp(t *testing.T) {
	sourceDir := t.TempDir()
	data := bytes.Repeat([]byte("small-resume-"), 70656) // 12 * 70656 = 847,872 bytes
	path := filepath.Join(sourceDir, "small.bin")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	relay := startTestRelay(t, 4)
	testDir := t.TempDir()
	receiveDir := filepath.Join(testDir, "receive")
	require.NoError(t, os.MkdirAll(receiveDir, 0o755))
	originalCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(receiveDir))
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })

	const chunkSize = int64(256 << 10)
	dest := filepath.Join(receiveDir, "small.bin")
	tempRelative := ".small.bin.crocpart-test"
	temp := filepath.Join(receiveDir, tempRelative)
	partial := append([]byte(nil), data[:2*chunkSize]...)
	partial = append(partial, make([]byte, len(data)-len(partial))...)
	require.NoError(t, os.WriteFile(temp, partial, 0o600))

	sourceHash, err := utils.HashFile(path, "xxhash", false)
	require.NoError(t, err)
	chunkCount := (int64(len(data)) + chunkSize - 1) / chunkSize
	meta := resume.Metadata{
		FileSize:      int64(len(data)),
		SourceHash:    sourceHash,
		HashAlgorithm: "xxhash",
		ChunkSize:     chunkSize,
		ChunkCount:    chunkCount,
		Destination:   "small.bin",
		TempPath:      tempRelative,
		StateVersion:  1,
	}
	journal := resume.New(meta)
	for i := int64(0); i < 2; i++ {
		start := i * chunkSize
		end := start + chunkSize
		journal.MarkComplete(i, xxhash.Sum64(data[start:end]))
	}
	require.NoError(t, os.WriteFile(dest+".crocstate", journal.Marshal(), 0o600))

	filesInfo, emptyFolders, folders, err := GetFilesInfo([]string{path}, false, false, nil)
	require.NoError(t, err)
	secret := fmt.Sprintf("small-resume-%d", time.Now().UnixNano())
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
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got))
	receiver.stats.mu.Lock()
	resumed := receiver.stats.resumed.Load()
	receiver.stats.mu.Unlock()
	assert.Equal(t, int64(2*chunkSize), resumed)
	_, err = os.Stat(temp)
	assert.True(t, os.IsNotExist(err), "temporary file must be renamed")
	_, err = os.Stat(dest + ".crocstate")
	assert.True(t, os.IsNotExist(err), "journal must be removed")
}
