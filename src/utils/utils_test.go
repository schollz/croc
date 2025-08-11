package utils

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const TCP_BUFFER_SIZE = 1024 * 64

var bigFileSize = 75000000

func bigFile() {
	os.WriteFile("bigfile.test", bytes.Repeat([]byte("z"), bigFileSize), 0o666)
}

func BenchmarkMD5(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MD5HashFile("bigfile.test", false)
	}
}

func BenchmarkXXHash(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		XXHashFile("bigfile.test", false)
	}
}

func BenchmarkImoHash(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IMOHashFile("bigfile.test")
	}
}

func BenchmarkHighwayHash(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HighwayHashFile("bigfile.test", false)
	}
}

func BenchmarkImoHashFull(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IMOHashFileFull("bigfile.test")
	}
}

func BenchmarkSha256(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SHA256("hello,world")
	}
}

func BenchmarkMissingChunks(b *testing.B) {
	bigFile()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MissingChunks("bigfile.test", int64(bigFileSize), TCP_BUFFER_SIZE/2)
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
	b, err := MD5HashFile("bigfile.test", false)
	assert.Nil(t, err)
	assert.Equal(t, "8304ff018e02baad0e3555bade29a405", fmt.Sprintf("%x", b))
	_, err = MD5HashFile("bigfile.test.nofile", false)
	assert.NotNil(t, err)
}

func TestHighwayHashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	b, err := HighwayHashFile("bigfile.test", false)
	assert.Nil(t, err)
	assert.Equal(t, "3c32999529323ed66a67aeac5720c7bf1301dcc5dca87d8d46595e85ff990329", fmt.Sprintf("%x", b))
	_, err = HighwayHashFile("bigfile.test.nofile", false)
	assert.NotNil(t, err)
}

func TestIMOHashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	b, err := IMOHashFile("bigfile.test")
	assert.Nil(t, err)
	assert.Equal(t, "c0d1e12301e6c635f6d4a8ea5c897437", fmt.Sprintf("%x", b))
}

func TestXXHashFile(t *testing.T) {
	bigFile()
	defer os.Remove("bigfile.test")
	b, err := XXHashFile("bigfile.test", false)
	assert.Nil(t, err)
	assert.Equal(t, "4918740eb5ccb6f7", fmt.Sprintf("%x", b))
	_, err = XXHashFile("nofile", false)
	assert.NotNil(t, err)
}

func TestSHA256(t *testing.T) {
	assert.Equal(t, "09ca7e4eaa6e8ae9c7d261167129184883644d07dfba7cbfbc4c8a2e08360d5b", SHA256("hello, world"))
}

func TestByteCountDecimal(t *testing.T) {
	assert.Equal(t, "10.0 kB", ByteCountDecimal(10240))
	assert.Equal(t, "50 B", ByteCountDecimal(50))
	assert.Equal(t, "12.4 MB", ByteCountDecimal(13002343))
}

func TestMissingChunks(t *testing.T) {
	fileSize := 100
	chunkSize := 10
	rand.Seed(1)
	bigBuff := make([]byte, fileSize)
	rand.Read(bigBuff)
	os.WriteFile("missing.test", bigBuff, 0o644)
	empty := make([]byte, chunkSize)
	f, err := os.OpenFile("missing.test", os.O_RDWR, 0o644)
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

	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example")
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
	chunkRanges = MissingChunks(tmpfile.Name(), int64(len(content)), chunkSize)
	assert.Empty(t, chunkRanges)
	chunkRanges = MissingChunks(tmpfile.Name(), int64(len(content)+10), chunkSize)
	assert.Empty(t, chunkRanges)
	chunkRanges = MissingChunks(tmpfile.Name()+"ok", int64(len(content)), chunkSize)
	assert.Empty(t, chunkRanges)
	chunks = ChunkRangesToChunks(chunkRanges)
	assert.Empty(t, chunks)
}

// func Test1(t *testing.T) {
// 	chunkRanges := MissingChunks("../../m/bigfile.test", int64(75000000), 1024*64/2)
// 	fmt.Println(chunkRanges)
// 	fmt.Println(ChunkRangesToChunks((chunkRanges)))
// 	assert.Nil(t, nil)
// }

func TestHashFile(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up

	if _, err = tmpfile.Write(content); err != nil {
		panic(err)
	}
	if err = tmpfile.Close(); err != nil {
		panic(err)
	}
	hashed, err := HashFile(tmpfile.Name(), "xxhash")
	assert.Nil(t, err)
	assert.Equal(t, "e66c561610ad51e2", fmt.Sprintf("%x", hashed))
}

func TestPublicIP(t *testing.T) {
	ip, err := PublicIP()
	fmt.Println(ip)
	assert.True(t, strings.Contains(ip, ".") || strings.Contains(ip, ":"))
	assert.Nil(t, err)
}

func TestLocalIP(t *testing.T) {
	ip := LocalIP()
	fmt.Println(ip)
	assert.True(t, strings.Contains(ip, ".") || strings.Contains(ip, ":"))
}

func TestGetRandomName(t *testing.T) {
	name := GetRandomName()
	fmt.Println(name)
	assert.NotEmpty(t, name)
}

func intSliceSame(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func TestFindOpenPorts(t *testing.T) {
	openPorts := FindOpenPorts("127.0.0.1", 9009, 4)
	if !intSliceSame(openPorts, []int{9009, 9010, 9011, 9012}) && !intSliceSame(openPorts, []int{9014, 9015, 9016, 9017}) {
		t.Errorf("openPorts: %v", openPorts)

	}
}

func TestIsLocalIP(t *testing.T) {
	assert.True(t, IsLocalIP("192.168.0.14:9009"))
}

func TestValidFileName(t *testing.T) {
	// contains regular characters
	assert.Nil(t, ValidFileName("中文.csl"))
	// contains regular characters
	assert.Nil(t, ValidFileName("[something].csl"))
	// contains regular characters
	assert.Nil(t, ValidFileName("[(something)].csl"))
	// contains invisible character
	err := ValidFileName("D中文.cslouglas​")
	assert.NotNil(t, err)
	assert.Equal(t, "non-graphical unicode: e2808b U+8203 in '44e4b8ade696872e63736c6f75676c6173e2808b'", err.Error())
	// contains "..", but not next to a path separator
	assert.Nil(t, ValidFileName("hi..txt"))
	// contains "..", but only next to a path separator on one side
	assert.Nil(t, ValidFileName("rel"+string(os.PathSeparator)+"..txt"))
	assert.Nil(t, ValidFileName("rel.."+string(os.PathSeparator)+"txt"))
	// contains ".." between two path separators, but does not break out of the base directory
	assert.Nil(t, ValidFileName("hi"+string(os.PathSeparator)+".."+string(os.PathSeparator)+"txt"))
	// contains ".." between two path separators, and breaks out of the base directory
	assert.NotNil(t, ValidFileName("hi"+string(os.PathSeparator)+".."+string(os.PathSeparator)+".."+string(os.PathSeparator)+"txt"))
	// contains ".." between a path separator and the beginning or end of the path
	assert.NotNil(t, ValidFileName(".."+string(os.PathSeparator)+"hi.txt"))
	assert.NotNil(t, ValidFileName("hi"+string(os.PathSeparator)+".."))
	assert.NotNil(t, ValidFileName(".."))
	// is an absolute path
	assert.NotNil(t, ValidFileName(path.Join(string(os.PathSeparator), "abs", string(os.PathSeparator), "hi.txt")))
}
