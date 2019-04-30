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
	assert.Equal(t, "6d39c2f3468e0d5869e0c9b349503175", fmt.Sprintf("%x", b))
}

// SHA256 returns sha256 sum
func TestSHA256(t *testing.T) {
	assert.Equal(t, "09ca7e4eaa6e8ae9c7d261167129184883644d07dfba7cbfbc4c8a2e08360d5b", SHA256("hello, world"))
}

func TestByteCountDecimal(t *testing.T) {
	assert.Equal(t, "10.0 kB", ByteCountDecimal(10000))
}

func TestMissingChunks(t *testing.T) {
	chunks := MissingChunks("test",11346432,1024 * 32)
	assert.Equal(t,202,len(chunks))
}
