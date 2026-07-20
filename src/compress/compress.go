package compress

import (
	"bytes"
	"compress/flate"
	"io"

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
	compress(src, compressedData, flate.HuffmanOnly)
	return compressedData.Bytes()
}

// Decompress returns a decompressed byte slice and any error encountered.
// Previously errors were silently swallowed and an empty slice returned,
// which caused json.Unmarshal to fail with "invalid character" when files
// with non-ASCII names (e.g. umlauts, accented chars) were transferred.
// Callers should fall back to the raw bytes if an error is returned.
func Decompress(src []byte) ([]byte, error) {
	compressedData := bytes.NewBuffer(src)
	deCompressedData := new(bytes.Buffer)
	decompressor := flate.NewReader(compressedData)
	defer decompressor.Close()
	if _, err := io.Copy(deCompressedData, decompressor); err != nil {
		log.Debugf("error decompressing data: %v", err)
		return nil, err
	}
	return deCompressedData.Bytes(), nil
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

// decompress uses flate to decompress an io.Reader (internal helper, kept for compatibility)
func decompress(src io.Reader, dest io.Writer) {
	decompressor := flate.NewReader(src)
	if _, err := io.Copy(dest, decompressor); err != nil {
		log.Debugf("error copying data: %v", err)
	}
	decompressor.Close()
}
