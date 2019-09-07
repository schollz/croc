package utils

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func bigFile() {
	rand.Seed(0)
	bigBuff := make([]byte, 75000000)
	rand.Read(bigBuff)
	ioutil.WriteFile("bigfile.test", bigBuff, 0666)
}

func BenchmarkMD5(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MD5HashFile("bigfile.test")
	}
}

func BenchmarkXXHash(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		XXHashFile("bigfile.test")
	}
}
func BenchmarkImoHash(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IMOHashFile("bigfile.test")
	}
}

func TestExists(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	fmt.Println(GetLocalIPs())
	assert.True(t, Exists("bigfile.test"))
	assert.False(t, Exists("doesnotexist"))
}

func TestMD5HashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	b, err := MD5HashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "9fed05acbacbc6a36555c998501c21f6", fmt.Sprintf("%x", b))
}

func TestIMOHashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	b, err := IMOHashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "c0d1e123a96a598ea801cc503d3db8c0", fmt.Sprintf("%x", b))
}

func TestXXHashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
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

func TestMissingChunks(t *testing.T) {
	fileSize := 100
	chunkSize := 10
	rand.Seed(1)
	bigBuff := make([]byte, fileSize)
	rand.Read(bigBuff)
	ioutil.WriteFile("missing.test", bigBuff, 0644)
	empty := make([]byte, chunkSize)
	f, err := os.OpenFile("missing.test", os.O_RDWR, 0644)
	assert.Nil(t, err)
	for block := 0; block < fileSize/chunkSize; block++ {
		if block == 0 || block == 4 || block == 5 || block >= 7 {
			f.WriteAt(empty, int64(block*chunkSize))
		}
	}
	f.Close()

	chunkRanges := MissingChunks("missing.test", int64(fileSize), chunkSize)
	assert.Equal(t, []int64{10, 0, 1, 40, 2, 70, 3}, chunkRanges)

	chunks := ChunkRangesToChunks(chunkRanges)
	assert.Equal(t, []int64{0, 40, 50, 70, 80, 90}, chunks)

	os.Remove("missing.test")
}

// func Test1(t *testing.T) {
// 	chunkRanges := MissingChunks("../../m/bigfile.test", int64(75000000), 1024*64/2)
// 	fmt.Println(chunkRanges)
// 	fmt.Println(ChunkRangesToChunks((chunkRanges)))
// 	assert.Nil(t, nil)
// }

func TestHashFile(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := ioutil.TempFile("", "example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write(content); err != nil {
		panic(err)
	}
	if err := tmpfile.Close(); err != nil {
		panic(err)
	}
	hashed, err := HashFile(tmpfile.Name())
	assert.Nil(t, err)
	assert.Equal(t, "18c9673a4bb8325d323e7f24fda9ae1e", fmt.Sprintf("%x", hashed))
}

func TestPublicIP(t *testing.T) {
	ip, err := PublicIP()
	fmt.Println(ip)
	assert.True(t, strings.Contains(ip, "."))
	assert.Nil(t, err)
}

func TestLocalIP(t *testing.T) {
	ip := LocalIP()
	fmt.Println(ip)
	assert.True(t, strings.Contains(ip, "."))
}

func TestGetRandomName(t *testing.T) {
	name := GetRandomName()
	assert.NotEmpty(t, name)
}
