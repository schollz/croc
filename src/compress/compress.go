package compress

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/zstd"

	log "github.com/schollz/logger"
)

// CompressWithOption returns compressed data using the specified level
func CompressWithOption(src []byte, level int) []byte {
	compressedData := new(bytes.Buffer)
	compress(src, compressedData, level)
	return compressedData.Bytes()
}

// Compress returns a compressed byte slice.
func Compress(src []byte) []byte {
	compressedData := new(bytes.Buffer)
	compress(src, compressedData, -2)
	return compressedData.Bytes()
}

// Decompress returns a decompressed byte slice.
func Decompress(src []byte) []byte {
	compressedData := bytes.NewBuffer(src)
	deCompressedData := new(bytes.Buffer)
	decompress(compressedData, deCompressedData)
	return deCompressedData.Bytes()
}

// compress uses zstd to compress a byte slice to a corresponding level
func compress(src []byte, dest io.Writer, level int) {
	compressor, err := zstd.NewWriter(dest, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	if err != nil {
		panic(err)
	}
	if _, err = compressor.Write(src); err != nil {
		log.Debugf("error writing data: %v", err)
	}
	compressor.Close()
}

// decompress uses zstd to decompress an io.Reader
func decompress(src io.Reader, dest io.Writer) {
	decompressor, err := zstd.NewReader(src)
	if err != nil {
		panic(err)
	}
	if _, err := io.Copy(dest, decompressor); err != nil {
		log.Debugf("error copying data: %v", err)
	}
	decompressor.Close()
}
