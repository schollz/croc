package croc

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

const (
	// Feature names are exchanged during PAKE and repeated in the file
	// manifest. Unknown names are ignored by older peers.
	parallelDataFeature       = "parallel-data-v2"
	chunkResumeFeature        = "chunk-resume-v2"
	parallelSmallFilesFeature = "parallel-small-files-v1"
	transferAutotuneFeature   = "transfer-autotune-v1"

	parallelFrameVersion       = byte(2)
	parallelFrameHeaderSize    = 24
	parallelFlagCompressed     = byte(1 << 0)
	parallelFlagFinal          = byte(1 << 1)
	parallelMaxChunkSize       = int64(4 << 20)
	parallelMinChunkSize       = int64(64 << 10)
	parallelDefaultChunkSize   = int64(256 << 10)
	parallelMaxBatchFiles      = 256
	parallelMaxBatchChunks     = 262144
	parallelSmallFileThreshold = int64(1 << 20)
	parallelMaxSegments        = 100000
)

// ParallelFileRequest asks the sender for the missing chunks of one file.
// Ranges and Bitmap are mutually exclusive. Bitmap bit i set means chunk i is
// missing.
type ParallelFileRequest struct {
	Index     int     `json:"i"`
	ChunkSize int64   `json:"cs"`
	Ranges    []int64 `json:"r,omitempty"`
	Bitmap    []byte  `json:"bm,omitempty"`
	Resume    bool    `json:"res,omitempty"`
}

// ParallelBatchReady starts one batch. The receiver never has more than one
// batch in flight.
type ParallelBatchReady struct {
	BatchID          uint64                `json:"b"`
	Files            []ParallelFileRequest `json:"f"`
	ReconnectVersion int                   `json:"rv,omitempty"`
}

// ParallelFileDone is sent by the receiver after a file is verified.
type ParallelFileDone struct {
	BatchID uint64 `json:"b"`
	Index   int    `json:"i"`
	Status  string `json:"s"`
	Bytes   int64  `json:"n,omitempty"`
	Hash    []byte `json:"h,omitempty"`
}

// ParallelBatchDone is sent by the sender after every file-done is received.
type ParallelBatchDone struct {
	BatchID uint64             `json:"b"`
	Status  string             `json:"s"`
	Files   []ParallelFileDone `json:"f,omitempty"`
}

// ParallelBatchReset lets the sender reject a batch before any data flows.
type ParallelBatchReset struct {
	BatchID       uint64 `json:"b"`
	Reason        string `json:"r"`
	MaxChunkSize  int64  `json:"mc,omitempty"`
	MaxBatchFiles int    `json:"mf,omitempty"`
}

const (
	parallelStatusOK           = "ok"
	parallelStatusSkipped      = "skipped"
	parallelStatusHashMismatch = "hash-mismatch"
	parallelStatusError        = "error"
)

// dataFrameV2 is the decoded plaintext header plus payload of a v2 data frame.
type dataFrameV2 struct {
	flags   byte
	batchID uint64
	fileID  uint32
	offset  int64
	payload []byte
}

func parallelChunkCount(fileSize, chunkSize int64) int64 {
	if fileSize <= 0 || chunkSize <= 0 {
		return 0
	}
	return (fileSize + chunkSize - 1) / chunkSize
}

func validParallelChunkSize(chunkSize, fileSize int64) bool {
	if chunkSize <= 0 || chunkSize > parallelMaxChunkSize {
		return false
	}
	// A file smaller than the minimum chunk uses one chunk equal to its size.
	if fileSize > 0 && chunkSize == fileSize {
		return true
	}
	if chunkSize < parallelMinChunkSize {
		return false
	}
	return bits.OnesCount64(uint64(chunkSize)) == 1
}

func validParallelRanges(ranges []int64, chunkSize, fileSize int64) error {
	count := parallelChunkCount(fileSize, chunkSize)
	if len(ranges) < 3 || len(ranges)%2 == 0 {
		return fmt.Errorf("invalid chunk ranges length %d", len(ranges))
	}
	if ranges[0] != chunkSize {
		return fmt.Errorf("range chunk size %d does not match %d", ranges[0], chunkSize)
	}
	var lastEnd int64 = -1
	total := int64(0)
	for i := 1; i+1 < len(ranges); i += 2 {
		start, n := ranges[i], ranges[i+1]
		if start < 0 || n <= 0 {
			return fmt.Errorf("invalid chunk range [%d,%d]", start, n)
		}
		if start <= lastEnd {
			return fmt.Errorf("chunk ranges are not strictly ascending")
		}
		if start+n > count {
			return fmt.Errorf("chunk range [%d,%d] exceeds chunk count %d", start, n, count)
		}
		lastEnd = start + n - 1
		total += n
	}
	if total == 0 {
		return fmt.Errorf("empty chunk request")
	}
	if total > parallelMaxBatchChunks {
		return fmt.Errorf("too many chunks in file request: %d", total)
	}
	return nil
}

func missingRangesFromBitmap(bitmap []byte, chunkSize, fileSize int64) ([]int64, error) {
	count := parallelChunkCount(fileSize, chunkSize)
	if int64(len(bitmap)) != (count+7)/8 {
		return nil, fmt.Errorf("bitmap length %d does not match chunk count %d", len(bitmap), count)
	}
	if count%8 != 0 {
		if bitmap[len(bitmap)-1]&byte(0xff<<uint(count%8)) != 0 {
			return nil, fmt.Errorf("bitmap has stray bits past chunk count")
		}
	}
	ranges := []int64{chunkSize}
	start := int64(-1)
	var run int64
	flush := func() {
		if run == 0 {
			return
		}
		ranges = append(ranges, start, run)
		run = 0
	}
	for i := range count {
		missing := bitmap[i/8]&(1<<uint(i%8)) != 0
		if !missing {
			flush()
			continue
		}
		if run == 0 {
			start = i
		}
		run++
	}
	flush()
	if len(ranges) == 1 {
		return nil, fmt.Errorf("empty bitmap requests no chunks")
	}
	return ranges, nil
}

func (r *ParallelFileRequest) normalize(info FileInfo) error {
	if r.Index < 0 {
		return fmt.Errorf("negative file index %d", r.Index)
	}
	if r.ChunkSize <= 0 || r.ChunkSize > parallelMaxChunkSize {
		return fmt.Errorf("invalid chunk size %d", r.ChunkSize)
	}
	if !validParallelChunkSize(r.ChunkSize, info.Size) {
		return fmt.Errorf("chunk size %d is not valid for file size %d", r.ChunkSize, info.Size)
	}
	hasRanges := len(r.Ranges) > 0
	hasBitmap := len(r.Bitmap) > 0
	if hasRanges == hasBitmap {
		return fmt.Errorf("exactly one of ranges or bitmap is required")
	}
	var err error
	if hasBitmap {
		var ranges []int64
		ranges, err = missingRangesFromBitmap(r.Bitmap, r.ChunkSize, info.Size)
		if err != nil {
			return err
		}
		r.Ranges = ranges
		r.Bitmap = nil
	}
	return validParallelRanges(r.Ranges, r.ChunkSize, info.Size)
}

func (b *ParallelBatchReady) validate(files []FileInfo) error {
	if b.BatchID == 0 {
		return fmt.Errorf("batch ID must be non-zero")
	}
	if len(b.Files) == 0 {
		return fmt.Errorf("batch must contain at least one file")
	}
	if len(b.Files) > parallelMaxBatchFiles {
		return fmt.Errorf("batch contains too many files: %d", len(b.Files))
	}
	seen := make(map[int]struct{}, len(b.Files))
	var chunks int64
	for i := range b.Files {
		req := &b.Files[i]
		if req.Index < 0 || req.Index >= len(files) {
			return fmt.Errorf("file index %d is out of range", req.Index)
		}
		if _, ok := seen[req.Index]; ok {
			return fmt.Errorf("duplicate file index %d", req.Index)
		}
		seen[req.Index] = struct{}{}
		info := files[req.Index]
		if info.Size <= 0 || info.Symlink != "" {
			return fmt.Errorf("file %d is not a regular data file", req.Index)
		}
		if req.ChunkSize > parallelMaxChunkSize {
			return fmt.Errorf("chunk size %d exceeds limit", req.ChunkSize)
		}
		if err := req.normalize(info); err != nil {
			return fmt.Errorf("file %d: %w", req.Index, err)
		}
		chunks += int64(parallelChunkCount(info.Size, req.ChunkSize))
	}
	if chunks > parallelMaxBatchChunks {
		return fmt.Errorf("batch requests too many chunks: %d", chunks)
	}
	return nil
}

// encodeDataFrameV2 appends a plaintext v2 frame to dst. The caller encrypts
// the result as a single AEAD message.
func encodeDataFrameV2(dst []byte, flags byte, batchID uint64, fileID uint32, offset int64, payload []byte) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("negative offset %d", offset)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty data frame")
	}
	if flags & ^byte(parallelFlagCompressed|parallelFlagFinal) != 0 {
		return nil, fmt.Errorf("unknown frame flags 0x%x", flags)
	}
	start := len(dst)
	dst = append(dst, make([]byte, parallelFrameHeaderSize)...)
	frame := dst[start:]
	frame[0] = parallelFrameVersion
	frame[1] = flags
	binary.LittleEndian.PutUint64(frame[4:12], batchID)
	binary.LittleEndian.PutUint32(frame[12:16], fileID)
	binary.LittleEndian.PutUint64(frame[16:24], uint64(offset))
	dst = append(dst, payload...)
	return dst, nil
}

func decodeDataFrameV2(plain []byte, maxChunk int64) (dataFrameV2, error) {
	if len(plain) < parallelFrameHeaderSize+1 {
		return dataFrameV2{}, fmt.Errorf("data frame too short: %d", len(plain))
	}
	if plain[0] != parallelFrameVersion {
		return dataFrameV2{}, fmt.Errorf("unsupported data frame version %d", plain[0])
	}
	flags := plain[1]
	if flags & ^byte(parallelFlagCompressed|parallelFlagFinal) != 0 {
		return dataFrameV2{}, fmt.Errorf("unknown data frame flags 0x%x", flags)
	}
	if plain[2] != 0 || plain[3] != 0 {
		return dataFrameV2{}, fmt.Errorf("reserved frame bytes are not zero")
	}
	batchID := binary.LittleEndian.Uint64(plain[4:12])
	fileID := binary.LittleEndian.Uint32(plain[12:16])
	offset := binary.LittleEndian.Uint64(plain[16:24])
	if offset > uint64(^uint64(0)>>1) {
		return dataFrameV2{}, fmt.Errorf("frame offset overflows int64")
	}
	payload := plain[parallelFrameHeaderSize:]
	if int64(len(payload)) > maxChunk {
		return dataFrameV2{}, fmt.Errorf("data frame payload %d exceeds limit %d", len(payload), maxChunk)
	}
	return dataFrameV2{
		flags:   flags,
		batchID: batchID,
		fileID:  fileID,
		offset:  int64(offset),
		payload: payload,
	}, nil
}
