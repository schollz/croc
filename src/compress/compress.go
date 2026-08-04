package compress

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"

	log "github.com/schollz/logger"
)

// ErrDecompressedSizeExceeded indicates that decompressed data exceeded the
// caller-provided output limit.
var ErrDecompressedSizeExceeded = errors.New("decompressed data exceeds maximum size")

// CompressWithOption returns compressed data using the specified level
func CompressWithOption(src []byte, level int) []byte {
	compressedData := new(bytes.Buffer)
	compress(src, compressedData, level)
	return compressedData.Bytes()
}

// Compress returns a compressed byte slice.
func Compress(src []byte) []byte {
	compressedData := new(bytes.Buffer)
	compress(src, compressedData, flate.HuffmanOnly)
	return compressedData.Bytes()
}

// Decompress returns a decompressed byte slice no larger than maxOutputSize.
func Decompress(src []byte, maxOutputSize int64) ([]byte, error) {
	if maxOutputSize < 0 {
		return nil, fmt.Errorf("maximum decompressed size must be non-negative: %d", maxOutputSize)
	}

	decompressedData := new(bytes.Buffer)
	if err := decompress(bytes.NewReader(src), decompressedData, maxOutputSize); err != nil {
		return nil, err
	}
	return decompressedData.Bytes(), nil
}

// compress uses flate to compress a byte slice to a corresponding level
func compress(src []byte, dest io.Writer, level int) {
	compressor, err := flate.NewWriter(dest, level)
	if err != nil {
		log.Debugf("error level data: %v", err)
		return
	}
	if _, err := compressor.Write(src); err != nil {
		log.Debugf("error writing data: %v", err)
	}
	compressor.Close()
}

// decompress uses flate to decompress an io.Reader while bounding the number
// of bytes written to dest.
func decompress(src io.Reader, dest io.Writer, maxOutputSize int64) (err error) {
	decompressor := flate.NewReader(src)
	defer func() {
		if closeErr := decompressor.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close decompressor: %w", closeErr))
		}
	}()

	limited := &io.LimitedReader{R: decompressor, N: maxOutputSize}
	if _, err := io.Copy(dest, limited); err != nil {
		return fmt.Errorf("decompress data: %w", err)
	}

	// Probe the decompressor without writing another byte to distinguish output
	// that exactly matched the limit from output that would exceed it.
	var probe [1]byte
	n, readErr := io.ReadFull(decompressor, probe[:])
	if n > 0 {
		return ErrDecompressedSizeExceeded
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("decompress data: %w", readErr)
	}
	return nil
}
