package utils

import (
	"archive/zip"
	"bytes"
	"context"
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

// TestHashFileCtxNoCancellation tests HashFileCtx without cancellation
func TestHashFileCtxNoCancellation(t *testing.T) {
	// Use the same bigFile() function as other tests
	bigFile()
	defer os.Remove("bigfile.test")

	ctx := context.Background()

	// Test each algorithm - using the same expected values from existing tests
	tests := []struct {
		name      string
		algorithm string
		wantHash  string
	}{
		{
			name:      "MD5 hash",
			algorithm: "md5",
			wantHash:  "8304ff018e02baad0e3555bade29a405", // From TestMD5HashFile
		},
		{
			name:      "XXHash",
			algorithm: "xxhash",
			wantHash:  "4918740eb5ccb6f7", // From TestXXHashFile
		},
		{
			name:      "imohash",
			algorithm: "imohash",
			wantHash:  "c0d1e12301e6c635f6d4a8ea5c897437", // From TestIMOHashFile
		},
		{
			name:      "highway",
			algorithm: "highway",
			wantHash:  "3c32999529323ed66a67aeac5720c7bf1301dcc5dca87d8d46595e85ff990329", // From TestHighwayHashFile
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test without progress bar
			hash, err := HashFileCtx(ctx, "bigfile.test", tt.algorithm)
			assert.NoError(t, err, "HashFileCtx should not return error")
			assert.Equal(t, tt.wantHash, fmt.Sprintf("%x", hash),
				"Hash should match for algorithm %s", tt.algorithm)

			// Test with progress bar (false)
			hash, err = HashFileCtx(ctx, "bigfile.test", tt.algorithm, false)
			assert.NoError(t, err, "HashFileCtx with showProgress=false should not return error")
			assert.Equal(t, tt.wantHash, fmt.Sprintf("%x", hash),
				"Hash should match for algorithm %s with showProgress=false", tt.algorithm)

			// Test with progress bar (true) - only for non-imohash to avoid spinner issues in tests
			if tt.algorithm != "imohash" {
				hash, err = HashFileCtx(ctx, "bigfile.test", tt.algorithm, true)
				assert.NoError(t, err, "HashFileCtx with showProgress=true should not return error")
				assert.Equal(t, tt.wantHash, fmt.Sprintf("%x", hash),
					"Hash should match for algorithm %s with showProgress=true", tt.algorithm)
			}
		})
	}

	// Test symlink handling - match original behavior
	t.Run("Symlink handling", func(t *testing.T) {
		// Create symlink to bigfile.test
		symlinkPath := "bigfile.test.symlink"
		defer os.Remove(symlinkPath)

		err := os.Symlink("bigfile.test", symlinkPath)
		if err != nil && strings.Contains(err.Error(), "privilege") {
			t.Skip("Skipping symlink test - requires privilege")
		}
		assert.NoError(t, err, "Should create symlink")

		// Hash the symlink
		hash, err := HashFileCtx(ctx, symlinkPath, "md5")
		assert.NoError(t, err, "Should hash symlink target path")
		assert.NotNil(t, hash, "Should return hash for symlink")

		// The original HashFile returns []byte(SHA256(target))
		// SHA256("bigfile.test") = "3ae29e98bba80ccefc79289c59cc34cb7223954310bb61c6a26147bb9b08c4e4"
		// []byte("3ae29e98...") = ASCII bytes of hex string

		// When converted back with fmt.Sprintf("%x", hash):
		// ASCII '3' = 0x33, 'a' = 0x61, 'e' = 0x65, '2' = 0x32, etc.
		// So fmt.Sprintf("%x", []byte("3ae2...")) = "33616532..."

		actualHex := fmt.Sprintf("%x", hash)

		// Let's compute what we SHOULD get:
		targetPath := "bigfile.test"
		expectedSHA256Hex := SHA256(targetPath) // "3ae29e98..."
		expectedBytes := []byte(expectedSHA256Hex)
		expectedResultHex := fmt.Sprintf("%x", expectedBytes) // hex of ASCII bytes

		// Debug
		t.Logf("Target path: '%s'", targetPath)
		t.Logf("SHA256(target) hex: %s", expectedSHA256Hex)
		t.Logf("Expected result (hex of ASCII bytes): %s", expectedResultHex)
		t.Logf("Actual result: %s", actualHex)

		// They should match!
		assert.Equal(t, expectedResultHex, actualHex,
			"HashFileCtx should behave exactly like HashFile for symlinks")

		// Also test with original HashFile to ensure consistency
		originalHash, err := HashFile(symlinkPath, "md5")
		assert.NoError(t, err)
		originalHex := fmt.Sprintf("%x", originalHash)

		assert.Equal(t, originalHex, actualHex,
			"HashFileCtx should return same result as HashFile for symlinks")
	})
	// Test error cases
	t.Run("Error cases", func(t *testing.T) {
		// Non-existent file
		hash, err := HashFileCtx(ctx, "non_existent_file_12345.test", "md5")
		assert.Error(t, err, "Should return error for non-existent file")
		assert.Nil(t, hash, "Hash should be nil on error")

		// Unsupported algorithm
		hash, err = HashFileCtx(ctx, "bigfile.test", "unsupported_algo")
		assert.Error(t, err, "Should return error for unsupported algorithm")
		assert.Contains(t, err.Error(), "unsupported algorithm")
		assert.Nil(t, hash, "Hash should be nil on error")
	})
}

// TestHashFileCtxWithCancellation tests HashFileCtx with context cancellation
func TestHashFileCtxWithCancellation(t *testing.T) {
	// Use the same bigFile() function
	bigFile()
	defer os.Remove("bigfile.test")

	// Test 1: Cancel before starting
	t.Run("Cancel before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		hash, err := HashFileCtx(ctx, "bigfile.test", "md5")
		assert.Error(t, err, "Should return error when context cancelled before start")
		assert.Equal(t, context.Canceled, err, "Error should be context.Canceled")
		assert.Nil(t, hash, "Hash should be nil when cancelled")
	})

	// Test 2: Cancel during operation
	t.Run("Cancel during operation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Start hash operation in goroutine
		errCh := make(chan error, 1)
		hashCh := make(chan []byte, 1)

		go func() {
			hash, err := HashFileCtx(ctx, "bigfile.test", "md5", false)
			if err != nil {
				errCh <- err
				hashCh <- nil
			} else {
				errCh <- nil
				hashCh <- hash
			}
		}()

		// Cancel after a short delay
		time.Sleep(10 * time.Millisecond)
		cancel()

		// Wait for result
		select {
		case err := <-errCh:
			hash := <-hashCh
			// Either we got an error (cancelled) or a hash (completed before cancellation)
			if err != nil {
				// Check if it's a context error
				if err == context.Canceled || err == context.DeadlineExceeded {
					assert.Error(t, err, "Should return context error when cancelled")
				}
				assert.Nil(t, hash, "Hash should be nil when cancelled")
			} else {
				// Completed successfully before cancellation
				assert.NotNil(t, hash, "If not cancelled, should return hash")
				assert.Equal(t, 16, len(hash), "MD5 hash should be 16 bytes")
				// Verify it's the correct hash
				assert.Equal(t, "8304ff018e02baad0e3555bade29a405", fmt.Sprintf("%x", hash))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Test timed out")
		}
	})

	// Test 3: Cancel with deadline
	t.Run("Cancel with deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// For a 75MB file, MD5 should take more than 1ms
		hash, err := HashFileCtx(ctx, "bigfile.test", "md5", false)
		assert.Error(t, err, "Should timeout for 75MB file with 1ms deadline")
		assert.Equal(t, context.DeadlineExceeded, err, "Error should be context.DeadlineExceeded")
		assert.Nil(t, hash, "Hash should be nil when deadline exceeded")
	})

	// Test 4: Imohash should be fast enough to complete before cancellation
	t.Run("Imohash fast completion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Imohash samples the file, so it should complete quickly
		hash, err := HashFileCtx(ctx, "bigfile.test", "imohash", false)
		assert.NoError(t, err, "Imohash should complete before any cancellation")
		assert.NotNil(t, hash, "Should return hash for imohash")
		assert.Equal(t, 16, len(hash), "Imohash should be 16 bytes")
		// Verify it's the correct hash
		assert.Equal(t, "c0d1e12301e6c635f6d4a8ea5c897437", fmt.Sprintf("%x", hash))
	})
}

// TestHashFileCtxEquivalence tests that HashFileCtx produces same results as original HashFile
func TestHashFileCtxEquivalence(t *testing.T) {
	// Use bigFile() for consistency
	bigFile()
	defer os.Remove("bigfile.test")

	algorithms := []string{"md5", "xxhash", "imohash", "highway"}

	for _, algorithm := range algorithms {
		t.Run(algorithm, func(t *testing.T) {
			// Get hash using original HashFile
			originalHash, err1 := HashFile("bigfile.test", algorithm)

			// Get hash using HashFileCtx with background context
			ctxHash, err2 := HashFileCtx(context.Background(), "bigfile.test", algorithm)

			// Both should succeed or fail together
			if err1 != nil {
				assert.Error(t, err2, "HashFileCtx should also fail if HashFile fails")
				t.Logf("Both failed as expected: %v", err1)
			} else {
				assert.NoError(t, err2, "HashFileCtx should not fail if HashFile succeeds")
				assert.NotNil(t, originalHash, "Original hash should not be nil")
				assert.NotNil(t, ctxHash, "Context hash should not be nil")

				// Compare hex representations
				originalHex := fmt.Sprintf("%x", originalHash)
				ctxHex := fmt.Sprintf("%x", ctxHash)
				assert.Equal(t, originalHex, ctxHex,
					"HashFile and HashFileCtx should produce same hash for algorithm %s. Got %s vs %s",
					algorithm, originalHex, ctxHex)

				// Also verify against known values from existing tests
				switch algorithm {
				case "md5":
					assert.Equal(t, "8304ff018e02baad0e3555bade29a405", originalHex)
				case "xxhash":
					assert.Equal(t, "4918740eb5ccb6f7", originalHex)
				case "imohash":
					assert.Equal(t, "c0d1e12301e6c635f6d4a8ea5c897437", originalHex)
				case "highway":
					assert.Equal(t, "3c32999529323ed66a67aeac5720c7bf1301dcc5dca87d8d46595e85ff990329", originalHex)
				}
			}
		})
	}
}

// TestHashFileCtxLargeFile tests with larger files (already using bigfile.test)
func TestHashFileCtxLargeFile(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// Use bigFile()
	bigFile()
	defer os.Remove("bigfile.test")

	ctx := context.Background()

	// Test each algorithm with large file
	algorithms := []string{"md5", "xxhash", "imohash", "highway"}

	for _, algorithm := range algorithms {
		t.Run(algorithm, func(t *testing.T) {
			hash, err := HashFileCtx(ctx, "bigfile.test", algorithm, false)
			assert.NoError(t, err, "Should hash large file with algorithm %s", algorithm)
			assert.NotNil(t, hash, "Should return hash for large file")

			// Verify hash size
			switch algorithm {
			case "md5":
				assert.Equal(t, 16, len(hash), "MD5 should be 16 bytes")
			case "xxhash":
				assert.Equal(t, 8, len(hash), "XXHash should be 8 bytes")
			case "imohash":
				assert.Equal(t, 16, len(hash), "Imohash should be 16 bytes")
			case "highway":
				assert.Equal(t, 32, len(hash), "HighwayHash should be 32 bytes")
			}

			// Verify against known values
			switch algorithm {
			case "md5":
				assert.Equal(t, "8304ff018e02baad0e3555bade29a405", fmt.Sprintf("%x", hash))
			case "xxhash":
				assert.Equal(t, "4918740eb5ccb6f7", fmt.Sprintf("%x", hash))
			case "imohash":
				assert.Equal(t, "c0d1e12301e6c635f6d4a8ea5c897437", fmt.Sprintf("%x", hash))
			case "highway":
				assert.Equal(t, "3c32999529323ed66a67aeac5720c7bf1301dcc5dca87d8d46595e85ff990329", fmt.Sprintf("%x", hash))
			}
		})
	}
}
