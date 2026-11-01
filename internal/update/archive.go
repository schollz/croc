package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// extractBinary reads the entire archive so duplicate entries cannot be hidden
// behind the first matching file. No archive paths are ever extracted to disk.
func extractBinary(asset string, archive []byte, name string, destination io.Writer) error {
	found := false
	seen := make(map[string]bool)
	check := func(entry string) error {
		if strings.Contains(entry, "\\") || strings.HasPrefix(entry, "/") || path.Clean(entry) != strings.TrimSuffix(entry, "/") || entry == ".." || strings.HasPrefix(entry, "../") || seen[entry] {
			return fmt.Errorf("update archive contains an unsafe or duplicate entry %q", entry)
		}
		seen[entry] = true
		return nil
	}
	copyBinary := func(source io.Reader, size int64) error {
		if size < 0 || size > maxUpdateBinarySize {
			return errors.New("update executable is too large or has an invalid size")
		}
		written, err := io.Copy(destination, io.LimitReader(source, maxUpdateBinarySize+1))
		if err != nil {
			return err
		}
		if written != size {
			return errors.New("update archive contains a truncated executable")
		}
		found = true
		return nil
	}
	if strings.HasSuffix(asset, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return err
		}
		for _, file := range reader.File {
			if err := check(file.Name); err != nil {
				return err
			}
			if !file.Mode().IsRegular() && !file.FileInfo().IsDir() {
				return errors.New("update archive contains a link or special file")
			}
			if file.Name != name {
				continue
			}
			if !file.Mode().IsRegular() || file.UncompressedSize64 > maxUpdateBinarySize {
				return errors.New("update archive contains an invalid executable")
			}
			source, err := file.Open()
			if err != nil {
				return err
			}
			copyErr := copyBinary(source, int64(file.UncompressedSize64))
			if err := errors.Join(copyErr, source.Close()); err != nil {
				return err
			}
		}
	} else {
		reader, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return err
		}
		defer reader.Close()
		// Bound decompression too, including entries other than the executable.
		bounded := &io.LimitedReader{R: reader, N: 2*maxUpdateArchiveSize + 1}
		tarReader := tar.NewReader(bounded)
		var total int64
		for {
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if err := check(header.Name); err != nil {
				return err
			}
			if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
				return errors.New("update archive contains a link or special file")
			}
			total += header.Size
			if header.Size < 0 || total > 2*maxUpdateArchiveSize {
				return errors.New("unpacked update archive is too large")
			}
			if header.Name != name {
				continue
			}
			if !header.FileInfo().Mode().IsRegular() {
				return errors.New("update archive contains an invalid executable")
			}
			if err := copyBinary(tarReader, header.Size); err != nil {
				return err
			}
		}
		// tar end markers can precede gzip trailer validation.
		if _, err := io.Copy(io.Discard, bounded); err != nil {
			return err
		}
		if bounded.N == 0 {
			return errors.New("unpacked update archive is too large")
		}
	}
	if !found {
		return fmt.Errorf("update archive does not contain %s", name)
	}
	return nil
}
