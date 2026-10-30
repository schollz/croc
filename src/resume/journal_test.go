package resume

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMeta() Metadata {
	return Metadata{
		FileSize:      1000,
		ModTime:       1234,
		SourceHash:    []byte{1, 2, 3},
		HashAlgorithm: "xxhash",
		ChunkSize:     250,
		ChunkCount:    4,
		Destination:   "file.bin",
		StateVersion:  1,
	}
}

func TestJournalRoundTrip(t *testing.T) {
	j := New(testMeta())
	j.MarkComplete(0, 10)
	j.MarkComplete(2, 30)
	data := j.Marshal()

	decoded, err := Parse(data)
	require.NoError(t, err)
	assert.True(t, decoded.Metadata().Matches(testMeta()))
	assert.True(t, decoded.IsComplete(0))
	assert.False(t, decoded.IsComplete(1))
	assert.Equal(t, uint64(30), decoded.ChunkHash(2))
	assert.Equal(t, []int64{250, 1, 1, 3, 1}, decoded.MissingRanges())
}

func TestJournalRejectsCorruptionAndMismatch(t *testing.T) {
	j := New(testMeta())
	j.MarkComplete(1, 1)
	data := j.Marshal()

	corrupt := append([]byte(nil), data...)
	corrupt[len(corrupt)/2] ^= 0xff
	_, err := Parse(corrupt)
	assert.Error(t, err)

	_, err = Parse([]byte("short"))
	assert.Error(t, err)

	other := testMeta()
	other.SourceHash = []byte{9}
	assert.False(t, other.Matches(j.Metadata()))
}

func TestJournalVerifyCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	chunk := make([]byte, 100)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	content := bytes.Repeat(chunk, 4)
	require.NoError(t, os.WriteFile(path, content, 0o600))

	meta := Metadata{FileSize: 400, ChunkSize: 100, ChunkCount: 4, SourceHash: []byte{1}}
	j := New(meta)
	j.MarkComplete(0, xxhash.Sum64(content[:100]))
	j.MarkComplete(1, xxhash.Sum64(content[100:200]))
	j.MarkComplete(2, xxhash.Sum64([]byte("wrong")))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	cleared, err := j.VerifyCompleted(f, "full")
	require.NoError(t, err)
	assert.Equal(t, 1, cleared)
	assert.True(t, j.IsComplete(0))
	assert.True(t, j.IsComplete(1))
	assert.False(t, j.IsComplete(2))
}

func TestJournalRejectsOversizedChunkCount(t *testing.T) {
	meta := testMeta()
	meta.ChunkCount = maxChunks + 1
	j := New(meta)
	_, err := Parse(j.Marshal())
	assert.Error(t, err)
}
