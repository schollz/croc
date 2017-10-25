package main

import (
	"os"
	"testing"
)

func TestSplitFile(t *testing.T) {
	err := SplitFile("testing_data/README.md", 3)
	if err != nil {
		t.Error(err)
	}
	os.Remove("testing_data/README.md.0")
	os.Remove("testing_data/README.md.1")
}

func TestFileSize(t *testing.T) {
	t.Run("File is ok ", func(t *testing.T) {
		_, err := FileSize("testing_data/README.md")
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
	})
	t.Run("File does not exist", func(t *testing.T) {
		s, err := FileSize("testing_data/someStrangeFile")
		if err == nil {
			t.Error("should return an error")
		}
		if s > 0 {
			t.Errorf("size should be 0, got: %d", s)
		}
	})
}

func TestHashFile(t *testing.T) {
	t.Run("Hash created successfully", func(t *testing.T) {
		h, err := HashFile("testing_data/README.md")
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
		if len(h) != 32 {
			t.Errorf("invalid md5 hash, length should be 32 got: %d", len(h))
		}
	})
	t.Run("File does not exist", func(t *testing.T) {
		h, err := HashFile("testing_data/someStrangeFile")
		if err == nil {
			t.Error("should return an error")
		}
		if len(h) > 0 {
			t.Errorf("hash length should be 0, got: %d", len(h))
		}
		if h != "" {
			t.Errorf("hash should be empty string, got: %s", h)
		}
	})
}
