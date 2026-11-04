package resume

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"

	"github.com/cespare/xxhash/v2"
)

const (
	journalMagic   = "CROCRSV2"
	journalVersion = 1
	maxChunks      = 1 << 28
)

var (
	ErrNoJournal = errors.New("resume journal not found")
	ErrCorrupt   = errors.New("resume journal is corrupt")
	ErrMismatch  = errors.New("resume journal does not match transfer")
)

// Metadata identifies the transfer state recorded by a journal.
type Metadata struct {
	FileSize      int64  `json:"file_size"`
	ModTime       int64  `json:"mod_time,omitempty"`
	SourceHash    []byte `json:"source_hash"`
	HashAlgorithm string `json:"hash_algorithm,omitempty"`
	ChunkSize     int64  `json:"chunk_size"`
	ChunkCount    int64  `json:"chunk_count"`
	Destination   string `json:"destination,omitempty"`
	TempPath      string `json:"temp_path,omitempty"`
	StateVersion  int    `json:"state_version"`
}

// Matches reports whether two metadata sets describe the same resumable file.
// ModTime is intentionally excluded: rsync/chmod may change it without
// invalidating already received blocks.
func (m Metadata) Matches(other Metadata) bool {
	return m.FileSize == other.FileSize &&
		m.ChunkSize == other.ChunkSize &&
		m.ChunkCount == other.ChunkCount &&
		bytes.Equal(m.SourceHash, other.SourceHash) &&
		m.HashAlgorithm == other.HashAlgorithm
}

type journalHeader struct {
	Metadata
	HasHashes bool `json:"has_hashes"`
}

// Journal is an in-memory snapshot of resumable chunk state. Callers persist it
// with Marshal after syncing the data file.
type Journal struct {
	meta    Metadata
	bitmap  *Bitmap
	hashes  []uint64
	hasHash bool
}

// New creates an empty journal where every chunk is missing.
func New(meta Metadata) *Journal {
	if meta.ChunkCount < 0 {
		meta.ChunkCount = 0
	}
	return &Journal{
		meta:    meta,
		bitmap:  NewBitmap(meta.ChunkCount),
		hashes:  make([]uint64, meta.ChunkCount),
		hasHash: true,
	}
}

// Metadata returns a copy of the journal metadata.
func (j *Journal) Metadata() Metadata { return j.meta }

// Count returns the number of completed chunks.
func (j *Journal) Count() int64 {
	if j == nil || j.bitmap == nil {
		return 0
	}
	return j.bitmap.Count()
}

// IsComplete reports whether chunk i is recorded as complete.
func (j *Journal) IsComplete(i int64) bool { return j != nil && j.bitmap.IsSet(i) }

// MarkComplete records chunk i and its content hash.
func (j *Journal) MarkComplete(i int64, hash uint64) {
	if j == nil {
		return
	}
	j.bitmap.Set(i)
	if i >= 0 && i < int64(len(j.hashes)) {
		j.hashes[i] = hash
		j.hasHash = true
	}
}

// ClearComplete removes chunk i from the completed set.
func (j *Journal) ClearComplete(i int64) {
	if j == nil {
		return
	}
	j.bitmap.Clear(i)
}

// Reset clears every completed chunk and hash.
func (j *Journal) Reset() {
	if j == nil {
		return
	}
	j.bitmap.ClearAll()
	for i := range j.hashes {
		j.hashes[i] = 0
	}
}

// MissingRanges returns the missing chunk ranges using the protocol format.
func (j *Journal) MissingRanges() []int64 {
	if j == nil {
		return nil
	}
	return j.bitmap.MissingRanges(j.meta.ChunkSize, j.meta.FileSize)
}

// Missing reports whether any chunk is still missing.
func (j *Journal) Missing() bool { return j == nil || j.Count() < j.meta.ChunkCount }

// ChunkHash returns the recorded hash for chunk i.
func (j *Journal) ChunkHash(i int64) uint64 {
	if j == nil || i < 0 || i >= int64(len(j.hashes)) {
		return 0
	}
	return j.hashes[i]
}

// Marshal serializes the journal with a header CRC and a whole-file CRC.
func (j *Journal) Marshal() []byte {
	header := journalHeader{Metadata: j.meta, HasHashes: j.hasHash}
	headerBytes, _ := json.Marshal(header)
	bitmapBytes := j.bitmap.Bytes()
	var hashes []byte
	if j.hasHash {
		hashes = make([]byte, 8*len(j.hashes))
		for i, h := range j.hashes {
			binary.LittleEndian.PutUint64(hashes[i*8:], h)
		}
	}
	size := 8 + 2 + 2 + 4 + len(headerBytes) + len(bitmapBytes) + len(hashes) + 4
	out := make([]byte, 0, size)
	out = append(out, journalMagic...)
	out = binary.LittleEndian.AppendUint16(out, journalVersion)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(headerBytes)))
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(headerBytes))
	out = append(out, headerBytes...)
	out = append(out, bitmapBytes...)
	out = append(out, hashes...)
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(out))
	return out
}

// Parse validates and decodes a serialized journal.
func Parse(data []byte) (*Journal, error) {
	const fixed = 8 + 2 + 2 + 4
	if len(data) < fixed+4 {
		return nil, fmt.Errorf("%w: file too short", ErrCorrupt)
	}
	if string(data[:8]) != journalMagic {
		return nil, fmt.Errorf("%w: bad magic", ErrCorrupt)
	}
	version := binary.LittleEndian.Uint16(data[8:10])
	if version != journalVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrCorrupt, version)
	}
	headerLen := int(binary.LittleEndian.Uint16(data[10:12]))
	headerCRC := binary.LittleEndian.Uint32(data[12:16])
	if headerLen <= 0 || fixed+headerLen+4 > len(data) {
		return nil, fmt.Errorf("%w: invalid header length", ErrCorrupt)
	}
	headerBytes := data[fixed : fixed+headerLen]
	if crc32.ChecksumIEEE(headerBytes) != headerCRC {
		return nil, fmt.Errorf("%w: header checksum mismatch", ErrCorrupt)
	}
	var header journalHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: decode header: %v", ErrCorrupt, err)
	}
	if header.ChunkCount < 0 || header.ChunkCount > maxChunks || header.ChunkSize <= 0 {
		return nil, fmt.Errorf("%w: invalid chunk metadata", ErrCorrupt)
	}
	bitmapLen := int((header.ChunkCount + 7) / 8)
	hashLen := 0
	if header.HasHashes {
		hashLen = int(header.ChunkCount) * 8
	}
	if fixed+headerLen+bitmapLen+hashLen+4 != len(data) {
		return nil, fmt.Errorf("%w: length does not match header", ErrCorrupt)
	}
	payloadEnd := len(data) - 4
	if crc32.ChecksumIEEE(data[:payloadEnd]) != binary.LittleEndian.Uint32(data[payloadEnd:]) {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrCorrupt)
	}
	bitmap, err := NewBitmapFromBytes(data[fixed+headerLen:fixed+headerLen+bitmapLen], header.ChunkCount)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	j := &Journal{meta: header.Metadata, bitmap: bitmap, hasHash: header.HasHashes}
	if header.HasHashes {
		j.hashes = make([]uint64, header.ChunkCount)
		start := fixed + headerLen + bitmapLen
		for i := range j.hashes {
			j.hashes[i] = binary.LittleEndian.Uint64(data[start+i*8:])
		}
	}
	return j, nil
}

// VerifyCompleted re-hashes completed chunks and clears any that do not match
// the journal. mode is "full", "sample", or "none".
func (j *Journal) VerifyCompleted(file *os.File, mode string) (int, error) {
	if j == nil || file == nil || mode == "none" {
		return 0, nil
	}
	var cleared int
	buf := make([]byte, j.meta.ChunkSize)
	for i := int64(0); i < j.meta.ChunkCount; i++ {
		if !j.bitmap.IsSet(i) {
			continue
		}
		if mode == "sample" && i%16 != 0 && i < j.meta.ChunkCount-256 {
			continue
		}
		offset := i * j.meta.ChunkSize
		length := j.meta.ChunkSize
		if remaining := j.meta.FileSize - offset; remaining < length {
			length = remaining
		}
		if length <= 0 {
			j.bitmap.Clear(i)
			cleared++
			continue
		}
		n, err := file.ReadAt(buf[:length], offset)
		if err != nil && n != int(length) {
			return cleared, fmt.Errorf("verify chunk %d: %w", i, err)
		}
		if xxhash.Sum64(buf[:n]) != j.hashes[i] {
			j.bitmap.Clear(i)
			cleared++
		}
	}
	return cleared, nil
}
