package croc

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataFrameV2RoundTrip(t *testing.T) {
	payload := []byte("hello world")
	plain, err := encodeDataFrameV2(nil, parallelFlagCompressed, 7, 3, 4096, payload)
	require.NoError(t, err)
	assert.Len(t, plain, parallelFrameHeaderSize+len(payload))
	assert.Equal(t, byte(parallelFrameVersion), plain[0])
	assert.Equal(t, parallelFlagCompressed, plain[1])
	assert.Equal(t, uint64(7), binary.LittleEndian.Uint64(plain[4:12]))
	assert.Equal(t, uint32(3), binary.LittleEndian.Uint32(plain[12:16]))
	assert.Equal(t, uint64(4096), binary.LittleEndian.Uint64(plain[16:24]))

	frame, err := decodeDataFrameV2(plain, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, parallelFlagCompressed, frame.flags)
	assert.Equal(t, uint64(7), frame.batchID)
	assert.Equal(t, uint32(3), frame.fileID)
	assert.Equal(t, int64(4096), frame.offset)
	assert.Equal(t, payload, frame.payload)
}

func TestDataFrameV2RejectsMalformed(t *testing.T) {
	payload := []byte("x")
	plain, err := encodeDataFrameV2(nil, 0, 1, 0, 0, payload)
	require.NoError(t, err)

	bad := append([]byte(nil), plain...)
	bad[0] = 3
	_, err = decodeDataFrameV2(bad, 1024)
	assert.Error(t, err)

	bad = append([]byte(nil), plain...)
	bad[1] = 0x80
	_, err = decodeDataFrameV2(bad, 1024)
	assert.Error(t, err)

	bad = append([]byte(nil), plain...)
	bad[2] = 1
	_, err = decodeDataFrameV2(bad, 1024)
	assert.Error(t, err)

	_, err = decodeDataFrameV2(plain[:parallelFrameHeaderSize], 1024)
	assert.Error(t, err)
	_, err = decodeDataFrameV2(plain, 0)
	assert.Error(t, err)
	_, err = encodeDataFrameV2(nil, 0, 1, 0, -1, payload)
	assert.Error(t, err)
	_, err = encodeDataFrameV2(nil, 0, 1, 0, 0, nil)
	assert.Error(t, err)
}

func TestParallelBatchReadyValidation(t *testing.T) {
	files := []FileInfo{
		{Name: "a", Size: 1 << 20},
		{Name: "b", Size: 4 << 20},
		{Name: "empty", Size: 0},
	}
	ready := ParallelBatchReady{
		BatchID: 1,
		Files: []ParallelFileRequest{
			{Index: 0, ChunkSize: 256 << 10, Ranges: []int64{256 << 10, 0, 4}},
		},
	}
	require.NoError(t, ready.validate(files))

	ready.Files[0].Ranges = []int64{256 << 10, 0, 5}
	assert.Error(t, ready.validate(files))

	ready.Files[0].Ranges = []int64{256 << 10, 0, 2, 1, 2}
	assert.Error(t, ready.validate(files), "overlapping ranges must be rejected")

	ready.Files = []ParallelFileRequest{{Index: 0, ChunkSize: 256 << 10, Ranges: []int64{256 << 10, 0, 4}}, {Index: 0, ChunkSize: 256 << 10, Ranges: []int64{256 << 10, 0, 4}}}
	assert.Error(t, ready.validate(files), "duplicate indices must be rejected")

	ready.Files = []ParallelFileRequest{{Index: 2, ChunkSize: 1, Ranges: []int64{1, 0, 1}}}
	assert.Error(t, ready.validate(files), "empty files cannot enter a data batch")

	ready.Files = []ParallelFileRequest{{Index: 0, ChunkSize: 256 << 10, Bitmap: []byte{0x0f}}}
	require.NoError(t, ready.Files[0].normalize(files[0]))
	assert.Equal(t, []int64{256 << 10, 0, 4}, ready.Files[0].Ranges)
}

func TestValidParallelChunkSize(t *testing.T) {
	assert.True(t, validParallelChunkSize(64<<10, 1<<20))
	assert.True(t, validParallelChunkSize(1<<20, 1<<20))
	assert.True(t, validParallelChunkSize(1000, 1000))
	assert.False(t, validParallelChunkSize(1000, 2000))
	assert.False(t, validParallelChunkSize(96<<10, 1<<20))
	assert.False(t, validParallelChunkSize(8<<20, 16<<20))
}
