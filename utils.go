package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"math"
	math_rand "math/rand"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/schollz/bytetoword"
)

// HashWords returns word after hashing
func HashWords(s string) string {
	hasher := md5.New()
	hasher.Write([]byte(s))
	return bytetoword.EncodeToString(hasher.Sum(nil))
}

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
		_, err = io.Copy(finished, fh)
		if err != nil {
			return errors.Wrap(err, "CatFiles copy: ")
		}
		fh.Close()
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

	buf := make([]byte, bytesPerPiece)
	for i := 0; i < numPieces; i++ {

		out, err := os.Create(fileName + "." + strconv.Itoa(i))
		if err != nil {
			return err
		}
		n, err := file.Read(buf)
		out.Write(buf[:n])
		out.Close()

		if err == io.EOF {
			break
		}
	}
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

// GetLocalIP returns the local ip address
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	bestIP := ""
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return bestIP
}

// src is seeds the random generator for generating random strings
var src = math_rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// RandStringBytesMaskImprSrc prints a random string
func RandStringBytesMaskImprSrc(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}
