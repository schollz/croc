package utils

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	assert.NotNil(t, ValidFileName("hi"+string(os.PathSeparator)+".."+string(os.PathSeparator)+".."+string(os.PathSeparator)+"hi.txt"))
	assert.NotNil(t, ValidFileName(".."))
	// is an absolute path
	assert.NotNil(t, ValidFileName(path.Join(string(os.PathSeparator), "abs", string(os.PathSeparator), "hi.txt")))
}

// zip

// TestUnzipDirectory tests the unzip directory functionality
func TestUnzipDirectory(t *testing.T) {
	// Create temporary directory for tests
	tmpDir, err := os.MkdirTemp("", "unzip_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test zip and extraction directory
	zipPath := filepath.Join(tmpDir, "test.zip")
	extractDir := filepath.Join(tmpDir, "extracted")

	// Create test zip file with proper structure and known mod times
	expectedModTime := time.Date(2023, 2, 1, 10, 30, 0, 0, time.UTC)
	if err := createTestZipWithModTime(zipPath, expectedModTime); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Test extraction
	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed: %v", err)
	}

	// Update expected files to match the actual structure from createTestZipWithModTime
	baseName := "test"
	expectedFiles := []string{
		baseName + "/file1.txt",
		baseName + "/subdir/file2.txt",
		baseName + "/subdir2/file3.txt",
		baseName + "/file4.txt",
	}

	// Also check directories
	expectedDirs := []string{
		baseName + "/",
		baseName + "/subdir/",
		baseName + "/subdir2/",
	}

	// Verify files
	for _, expectedFile := range expectedFiles {
		fullPath := filepath.Join(extractDir, expectedFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("File was not extracted: %s", expectedFile)
		} else {
			// Verify modification time is preserved after extraction
			verifyFileModTime(t, fullPath, expectedModTime)
		}
	}

	// Verify directories
	for _, expectedDir := range expectedDirs {
		fullPath := filepath.Join(extractDir, expectedDir)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Directory was not extracted: %s", expectedDir)
		} else {
			// Verify modification time is preserved after extraction
			verifyFileModTime(t, fullPath, expectedModTime)
		}
	}

	// Verify file contents after extraction
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file1.txt"), "Test content 1")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/subdir/file2.txt"), "Test content 2")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/subdir2/file3.txt"), "Test content 3")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file4.txt"), "Test content 4")
}

// TestUnzipToNonExistentDirectory tests unzip to non-existent destination
func TestUnzipToNonExistentDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "unzip_nonexistent_dest_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test zip
	zipPath := filepath.Join(tmpDir, "test.zip")
	expectedModTime := time.Date(2023, 4, 1, 9, 0, 0, 0, time.UTC)
	if err := createTestZipWithModTime(zipPath, expectedModTime); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Extract to non-existent directory
	extractDir := filepath.Join(tmpDir, "nonexistent", "deep", "path")

	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed to create destination directory: %v", err)
	}

	// Update expected files to match the actual structure
	baseName := "test"
	expectedFiles := []string{
		baseName + "/file1.txt",
		baseName + "/subdir/file2.txt",
		baseName + "/subdir2/file3.txt",
		baseName + "/file4.txt",
	}

	// Also check directories
	expectedDirs := []string{
		baseName + "/",
		baseName + "/subdir/",
		baseName + "/subdir2/",
	}

	// Verify files
	for _, expectedFile := range expectedFiles {
		fullPath := filepath.Join(extractDir, expectedFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("File was not extracted to non-existent destination: %s", expectedFile)
		} else {
			// Verify modification time is preserved
			verifyFileModTime(t, fullPath, expectedModTime)
		}
	}

	// Verify directories
	for _, expectedDir := range expectedDirs {
		fullPath := filepath.Join(extractDir, expectedDir)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Directory was not extracted to non-existent destination: %s", expectedDir)
		} else {
			// Verify modification time is preserved
			verifyFileModTime(t, fullPath, expectedModTime)
		}
	}
}

// TestZipAndUnzipRoundTrip tests complete zip/unzip cycle with proper paths
func TestZipAndUnzipRoundTrip(t *testing.T) {
	// Create temporary directory for tests
	tmpDir, err := os.MkdirTemp("", "roundtrip_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory with test files
	sourceDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	// Use specific mod times for different items
	rootModTime := time.Date(2023, 3, 1, 14, 30, 0, 0, time.UTC)
	subdirModTime := time.Date(2023, 3, 1, 14, 29, 0, 0, time.UTC)
	subdir2ModTime := time.Date(2023, 3, 1, 14, 28, 0, 0, time.UTC)
	fileModTime := time.Date(2023, 3, 1, 14, 31, 0, 0, time.UTC)

	// Create directories structure first
	dirs := []string{
		"subdir",
		"subdir2",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(sourceDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
	}

	// Create files with specific modification times
	testFiles := map[string]string{
		"file1.txt":         "Content of file 1",
		"subdir/file2.txt":  "Content of file 2 in subdir",
		"subdir2/file3.txt": "Content of file 3 in another subdir",
		"file4.txt":         "Content of file 4",
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(sourceDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		if err := os.Chtimes(fullPath, fileModTime, fileModTime); err != nil {
			t.Fatalf("Failed to set file time: %v", err)
		}
	}

	// NOW set directory times AFTER creating all files

	// Set time for root source directory
	if err := os.Chtimes(sourceDir, rootModTime, rootModTime); err != nil {
		t.Fatalf("Failed to set source directory time: %v", err)
	}

	// Set times for subdirectories
	dirTimes := map[string]time.Time{
		"subdir":  subdirModTime,
		"subdir2": subdir2ModTime,
	}

	for dir, modTime := range dirTimes {
		fullPath := filepath.Join(sourceDir, dir)
		if err := os.Chtimes(fullPath, modTime, modTime); err != nil {
			t.Fatalf("Failed to set directory %s time: %v", dir, err)
		}
	}

	// Wait a moment to ensure time changes are applied
	time.Sleep(100 * time.Millisecond)

	// Create zip
	zipPath := filepath.Join(tmpDir, "test.zip")
	err = ZipDirectory(zipPath, sourceDir)
	if err != nil {
		t.Fatalf("ZipDirectory failed: %v", err)
	}

	// Print zip contents using Go's zip reader
	fmt.Printf("=== ZIP Archive Contents ===\n")
	archive, err := zip.OpenReader(zipPath)
	if err == nil {
		defer archive.Close()
		for _, f := range archive.File {
			modifiedTime := f.Modified
			if modifiedTime.IsZero() {
				modifiedTime = f.FileHeader.Modified
			}
			fmt.Printf("  %s (dir: %v) modTime: %v\n", f.Name, f.FileInfo().IsDir(), modifiedTime.UTC())
		}
	}

	// Extract to different directory
	extractDir := filepath.Join(tmpDir, "extracted")
	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed: %v", err)
	}

	// Expected items (both files and directories)
	baseName := "test"
	expectedItems := []string{
		baseName + "/",
		baseName + "/file1.txt",
		baseName + "/subdir/",
		baseName + "/subdir/file2.txt",
		baseName + "/subdir2/",
		baseName + "/subdir2/file3.txt",
		baseName + "/file4.txt",
	}

	expectedExtractedTimes := map[string]time.Time{
		baseName + "/":                  rootModTime,
		baseName + "/subdir/":           subdirModTime,
		baseName + "/subdir2/":          subdir2ModTime,
		baseName + "/file1.txt":         fileModTime,
		baseName + "/subdir/file2.txt":  fileModTime,
		baseName + "/subdir2/file3.txt": fileModTime,
		baseName + "/file4.txt":         fileModTime,
	}

	// Verify all items exist with correct modification times
	fmt.Printf("=== Extracted Files Verification ===\n")
	for _, itemPath := range expectedItems {
		fullPath := filepath.Join(extractDir, itemPath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Item was not extracted: %s", itemPath)
			continue
		}

		// Verify with test assertion
		expectedTime := expectedExtractedTimes[itemPath]
		verifyFileModTime(t, fullPath, expectedTime)
	}

	// Verify file contents
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file1.txt"), "Content of file 1")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/subdir/file2.txt"), "Content of file 2 in subdir")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/subdir2/file3.txt"), "Content of file 3 in another subdir")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file4.txt"), "Content of file 4")
}

// Helper function to create test zip file with specific modification time
func createTestZipWithModTime(zipPath string, modTime time.Time) error {
	file, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	// Get base name for consistent structure
	baseName := strings.TrimSuffix(filepath.Base(zipPath), ".zip")

	// First create entries for directories with modification time
	dirs := []string{
		baseName + "/",
		baseName + "/subdir/",
		baseName + "/subdir2/",
	}

	for _, dir := range dirs {
		header := &zip.FileHeader{
			Name:     filepath.ToSlash(dir),
			Modified: modTime,
		}
		_, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
	}

	// Then create files
	files := []struct {
		name    string
		content string
	}{
		{filepath.Join(baseName, "file1.txt"), "Test content 1"},
		{filepath.Join(baseName, "subdir", "file2.txt"), "Test content 2"},
		{filepath.Join(baseName, "subdir2", "file3.txt"), "Test content 3"},
		{filepath.Join(baseName, "file4.txt"), "Test content 4"},
	}

	for _, f := range files {
		header := &zip.FileHeader{
			Name:     filepath.ToSlash(f.name),
			Modified: modTime,
			Method:   zip.Deflate,
		}

		w, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			return err
		}
	}

	return nil
}

// Helper function to verify file content
func verifyFileContent(t *testing.T, filePath, expectedContent string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Errorf("Failed to read file %s: %v", filePath, err)
		return
	}

	if string(content) != expectedContent {
		t.Errorf("Content mismatch for %s, expected '%s', got '%s'",
			filePath, expectedContent, string(content))
	}
}

// Helper function to verify file modification time
func verifyFileModTime(t *testing.T, filePath string, expectedTime time.Time) {
	info, err := os.Stat(filePath)
	if err != nil {
		t.Errorf("Failed to stat file %s: %v", filePath, err)
		return
	}

	// Compare times truncated to seconds (file system precision may vary)
	expected := expectedTime.UTC().Truncate(time.Second)
	actual := info.ModTime().UTC().Truncate(time.Second)

	if !actual.Equal(expected) {
		t.Errorf("Modification time mismatch for %s, expected %v, got %v",
			filePath, expected, actual)
	}
}
