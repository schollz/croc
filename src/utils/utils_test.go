package utils

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func init() {
	rand.Seed(0)
	bigBuff := make([]byte, 75000000)
	rand.Read(bigBuff)
	ioutil.WriteFile("bigfile.test", bigBuff, 0666)
}

func BenchmarkMD5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		MD5HashFile("bigfile.test")
	}
}

func BenchmarkXXHash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		XXHashFile("bigfile.test")
	}
}
func BenchmarkImoHash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IMOHashFile("bigfile.test")
	}
}

func TestExists(t *testing.T) {
	assert.True(t, Exists("bigfile.test"))
	assert.False(t, Exists("doesnotexist"))
}

func TestMD5HashFile(t *testing.T) {
	b, err := MD5HashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "9fed05acbacbc6a36555c998501c21f6", fmt.Sprintf("%x", b))
}

func TestIMOHashFile(t *testing.T) {
	b, err := IMOHashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "c0d1e123a96a598ea801cc503d3db8c0", fmt.Sprintf("%x", b))
}

func TestXXHashFile(t *testing.T) {
	b, err := XXHashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "f2da6ee7e75e8324", fmt.Sprintf("%x", b))
}

func TestSHA256(t *testing.T) {
	assert.Equal(t, "09ca7e4eaa6e8ae9c7d261167129184883644d07dfba7cbfbc4c8a2e08360d5b", SHA256("hello, world"))
}

func TestByteCountDecimal(t *testing.T) {
	assert.Equal(t, "10.0 kB", ByteCountDecimal(10000))
}
