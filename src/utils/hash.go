package utils

import (
	"crypto/md5"
	"io"
	"os"
)

func HashFile(fname string) (hash256 []byte, err error) {
	f, err := os.Open("file.txt")
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
