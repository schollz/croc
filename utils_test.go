package main

import (
	"os"
	"testing"
)

func TestSplitFile(t *testing.T) {
	err := SplitFile("README.md", 3)
	if err != nil {
		t.Error(err)
	}
	os.Remove("README.md.0")
	os.Remove("README.md.1")
}
