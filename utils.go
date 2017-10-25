package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/pkg/errors"
)

// CatFiles copies data from n files to a single one and removes source files
// if Debug mode is set to false
func CatFiles(files []string, outfile string, remove bool) error {
	finished, err := os.Create(outfile)
	if err != nil {
		return errors.Wrap(err, "CatFiles create: ")
	}
	defer finished.Close()
	for _, file := range files {
		fh, err := os.Open(file)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("CatFiles open %v: ", file))
		}
		defer fh.Close()
		_, err = io.Copy(finished, fh)
		if err != nil {
			return errors.Wrap(err, "CatFiles copy: ")
		}
		if remove {
			os.Remove(file)
		}
	}
	return nil
}

// SplitFile creates a bunch of smaller files with the data from source splited into them
func SplitFile(fileName string, numPieces int) (err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return err
	}

	bytesPerPiece := int(math.Ceil(float64(fi.Size()) / float64(numPieces)))
	bytesRead := 0
	i := 0
	out, err := os.Create(fileName + "." + strconv.Itoa(i))
	if err != nil {
		return err
	}
	buf := make([]byte, 4096)
	if bytesPerPiece < 4096/numPieces {
		buf = make([]byte, bytesPerPiece)
	}
	for {
		n, err := file.Read(buf)
		out.Write(buf[:n])
		// If written bytes count is smaller than lenght of buffer
		// then we don't create one more empty file
		if err == io.EOF || n < len(buf) {
			break
		}
		bytesRead += n

		if bytesRead >= bytesPerPiece {
			// Close file and open a new one
			out.Close()
			i++
			out, err = os.Create(fileName + "." + strconv.Itoa(i))
			if err != nil {
				return err
			}
			bytesRead = 0
		}
	}
	out.Close()
	return nil
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

// HashFile does a md5 hash on the file
// from https://golang.org/pkg/crypto/md5/#example_New_file
func HashFile(filename string) (hash string, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()

	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return
	}
	hash = fmt.Sprintf("%x", h.Sum(nil))
	return
}

// FileSize returns the size of a file
func FileSize(filename string) (int, error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return -1, err
	}
	size := int(fi.Size())
	return size, nil
}
