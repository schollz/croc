package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// HashFile returns the md5 hash of a file
func HashFile(fname string) (hash256 []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return
	}

	hash256 = h.Sum(nil)
	return
}

// SHA256 returns sha256 sum
func SHA256(s string) string {
	sha := sha256.New()
	sha.Write([]byte(s))
	return fmt.Sprintf("%x", sha.Sum(nil))
}
