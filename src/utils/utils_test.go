package utils

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Exists reports whether the named file or directory exists.
func TestExists(t *testing.T) {
	assert.True(t, Exists("utils.go"))
}

// HashFile returns the md5 hash of a file
func TestHashFile(t *testing.T) {
	b, err := HashFile("utils.go")
	assert.Nil(t, err)
	assert.Equal(t, "314965d4170cd0c70bd2e4f6c977750a", fmt.Sprintf("%x", b))
}

// SHA256 returns sha256 sum
func TestSHA256(t *testing.T) {
	assert.Equal(t, "09ca7e4eaa6e8ae9c7d261167129184883644d07dfba7cbfbc4c8a2e08360d5b", SHA256("hello, world"))
}

func TestByteCountDecimal(t *testing.T) {
	assert.Equal(t, "10.0 kB", ByteCountDecimal(10000))
}
