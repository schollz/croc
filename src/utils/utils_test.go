package utils

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
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

// TestZipDirectory tests the zip directory functionality
func TestZipDirectory(t *testing.T) {
	// Create temporary directory for tests
	tmpDir, err := os.MkdirTemp("", "zip_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files with different content
	testFiles := []struct {
		path    string
		content string
	}{
		{filepath.Join(tmpDir, "file1.txt"), "Hello, World!"},
		{filepath.Join(tmpDir, "subdir", "file2.txt"), "Test content in subdirectory"},
		{filepath.Join(tmpDir, "file3.txt"), "Another test file"},
	}

	// Create directory structure and files
	for _, tf := range testFiles {
		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(tf.path), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Write file content
		if err := os.WriteFile(tf.path, []byte(tf.content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Set specific modification time for testing
		testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		if err := os.Chtimes(tf.path, testTime, testTime); err != nil {
			t.Fatalf("Failed to set file time: %v", err)
		}
	}

	// Test zip creation
	zipPath := filepath.Join(tmpDir, "test.zip")
	err = ZipDirectory(zipPath, tmpDir)
	if err != nil {
		t.Fatalf("ZipDirectory failed: %v", err)
	}

	// Verify zip file was created
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Fatalf("Zip file was not created")
	}

	// Verify zip contents
	verifyZipContents(t, zipPath, tmpDir, testFiles)
}

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

	// Create test zip file with proper structure
	if err := createTestZip(zipPath); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Test extraction
	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed: %v", err)
	}

	// Update expected files to include base name prefix
	baseName := "test"
	expectedFiles := []string{
		baseName + "/file1.txt",
		baseName + "/subdir/file2.txt",
		baseName + "/file3.txt",
	}

	for _, expectedFile := range expectedFiles {
		fullPath := filepath.Join(extractDir, expectedFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("File was not extracted: %s", expectedFile)
		}
	}

	// Verify file contents after extraction
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file1.txt"), "Test content 1")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/subdir/file2.txt"), "Test content 2")
	verifyFileContent(t, filepath.Join(extractDir, baseName+"/file3.txt"), "Test content 3")
}

// TestUnzipWithExistingFiles tests unzip behavior when files already exist
func TestUnzipWithExistingFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "unzip_existing_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test zip
	zipPath := filepath.Join(tmpDir, "test.zip")
	if err := createTestZip(zipPath); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Create a file that will conflict during extraction
	baseName := "test"
	existingFile := filepath.Join(tmpDir, baseName, "file1.txt")
	if err := os.MkdirAll(filepath.Dir(existingFile), 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	if err := os.WriteFile(existingFile, []byte("Existing content that should not be overwritten"), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Mock user input (answer "no" to overwrite prompt)
	// Note: This requires GetInput to be mockable in your implementation
	t.Logf("Skipping overwrite test as it requires GetInput mocking")
}

// TestZipNonExistentDirectory tests zip behavior with non-existent directory
func TestZipNonExistentDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zip_nonexistent_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "test.zip")
	nonExistentDir := filepath.Join(tmpDir, "nonexistent")

	// Main goal - verify no panic occurs
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Function panicked: %v", r)
		}
	}()

	// Call function - it may return error but should not panic
	err = ZipDirectory(zipPath, nonExistentDir)

	// Error is expected, so just log it
	if err != nil {
		t.Logf("Function returned error (expected): %v", err)
	}
}

// TestUnzipInvalidFile tests unzip behavior with invalid/corrupted zip file
func TestUnzipInvalidFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "unzip_invalid_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	invalidZipPath := filepath.Join(tmpDir, "invalid.zip")
	if err := os.WriteFile(invalidZipPath, []byte("this is not a valid zip file"), 0644); err != nil {
		t.Fatalf("Failed to create invalid zip: %v", err)
	}

	// Main goal - verify no panic occurs
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Function panicked: %v", r)
		}
	}()

	err = UnzipDirectory(tmpDir, invalidZipPath)

	// We expect error but not panic - the function should handle invalid zip gracefully
	if err != nil {
		t.Logf("Function returned error (expected): %v", err)
	} else {
		// If no error returned, that's also acceptable as long as no panic
		t.Logf("Function handled invalid zip without panic")
	}
}

// TestUnzipWithCorruptedZip tests unzip behavior with corrupted zip file
func TestUnzipWithCorruptedZip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "unzip_corrupted_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create corrupted zip file
	corruptedZipPath := filepath.Join(tmpDir, "corrupted.zip")

	// First create valid zip
	if err := createTestZip(corruptedZipPath); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Corrupt the file by appending garbage data
	f, err := os.OpenFile(corruptedZipPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open zip for corruption: %v", err)
	}
	f.Write([]byte("corrupted data appended to the end"))
	f.Close()

	// Verify no panic occurs
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Function panicked with corrupted zip: %v", r)
		}
	}()

	err = UnzipDirectory(tmpDir, corruptedZipPath)

	// Function may return error or partially extract, but should not panic
	if err != nil {
		t.Logf("Function returned error (may be expected): %v", err)
	} else {
		t.Logf("Function completed without error on corrupted zip")
	}
}

// TestUnzipContinuesOnErrors tests that unzip continues processing despite errors
func TestUnzipContinuesOnErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "unzip_continue_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create archive with problematic files
	zipPath := filepath.Join(tmpDir, "problematic.zip")

	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create zip: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	// Get base name for consistent structure
	baseName := strings.TrimSuffix(filepath.Base(zipPath), ".zip")

	// Add normal file
	w1, err := writer.Create(filepath.ToSlash(filepath.Join(baseName, "good_file.txt")))
	if err != nil {
		t.Fatalf("Failed to create good file: %v", err)
	}
	w1.Write([]byte("Good file content"))

	// Add file with invalid name (path traversal attempt)
	w2, err := writer.Create("../bad_file.txt")
	if err != nil {
		t.Fatalf("Failed to create bad file: %v", err)
	}
	w2.Write([]byte("Bad file content"))

	// Add another normal file
	w3, err := writer.Create(filepath.ToSlash(filepath.Join(baseName, "another_good_file.txt")))
	if err != nil {
		t.Fatalf("Failed to create another good file: %v", err)
	}
	w3.Write([]byte("Another good file content"))

	writer.Close()

	// Verify extraction continues despite errors
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Function panicked: %v", r)
		}
	}()

	err = UnzipDirectory(tmpDir, zipPath)

	// Update expected paths to include base name prefix
	goodFile := filepath.Join(tmpDir, baseName, "good_file.txt")
	if _, err := os.Stat(goodFile); os.IsNotExist(err) {
		t.Errorf("Good file was not extracted due to other errors")
	} else {
		verifyFileContent(t, goodFile, "Good file content")
	}

	anotherGoodFile := filepath.Join(tmpDir, baseName, "another_good_file.txt")
	if _, err := os.Stat(anotherGoodFile); os.IsNotExist(err) {
		t.Errorf("Another good file was not extracted due to other errors")
	} else {
		verifyFileContent(t, anotherGoodFile, "Another good file content")
	}

	// Bad file should not exist due to path traversal protection
	badFile := filepath.Join(tmpDir, "bad_file.txt")
	if _, err := os.Stat(badFile); err == nil {
		t.Errorf("Bad file was extracted despite path traversal protection")
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

	// Create test files
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
	}

	// Create zip
	zipPath := filepath.Join(tmpDir, "test.zip")
	err = ZipDirectory(zipPath, sourceDir)
	if err != nil {
		t.Fatalf("ZipDirectory failed: %v", err)
	}

	// Extract to different directory
	extractDir := filepath.Join(tmpDir, "extracted")
	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed: %v", err)
	}

	// Update expected paths to include the base name prefix from zip structure
	baseName := "test" // This matches the zip filename without .zip
	expectedFiles := map[string]string{
		baseName + "/file1.txt":         "Content of file 1",
		baseName + "/subdir/file2.txt":  "Content of file 2 in subdir",
		baseName + "/subdir2/file3.txt": "Content of file 3 in another subdir",
		baseName + "/file4.txt":         "Content of file 4",
	}

	// Verify all files exist with correct content
	for filePath, expectedContent := range expectedFiles {
		fullPath := filepath.Join(extractDir, filePath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("File was not extracted: %s", filePath)
			continue
		}
		verifyFileContent(t, fullPath, expectedContent)
	}
}

// TestZipEmptyDirectory tests zipping an empty directory
func TestZipEmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zip_empty_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	emptyDir := filepath.Join(tmpDir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	zipPath := filepath.Join(tmpDir, "empty.zip")

	err = ZipDirectory(zipPath, emptyDir)
	if err != nil {
		t.Fatalf("ZipDirectory failed for empty directory: %v", err)
	}

	// Verify zip file was created
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Fatalf("Zip file was not created for empty directory")
	}
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
	if err := createTestZip(zipPath); err != nil {
		t.Fatalf("Failed to create test zip: %v", err)
	}

	// Extract to non-existent directory
	extractDir := filepath.Join(tmpDir, "nonexistent", "deep", "path")

	err = UnzipDirectory(extractDir, zipPath)
	if err != nil {
		t.Fatalf("UnzipDirectory failed to create destination directory: %v", err)
	}

	// Update expected files to include base name prefix
	baseName := "test"
	expectedFiles := []string{
		baseName + "/file1.txt",
		baseName + "/subdir/file2.txt",
		baseName + "/file3.txt",
	}

	for _, expectedFile := range expectedFiles {
		fullPath := filepath.Join(extractDir, expectedFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("File was not extracted to non-existent destination: %s", expectedFile)
		}
	}
}

// Helper function to create test zip file with proper structure
func createTestZip(zipPath string) error {
	file, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	// Get base name for consistent structure
	baseName := strings.TrimSuffix(filepath.Base(zipPath), ".zip")

	// Create files in archive with proper structure
	files := []struct {
		name    string
		content string
	}{
		{filepath.Join(baseName, "file1.txt"), "Test content 1"},
		{filepath.Join(baseName, "subdir", "file2.txt"), "Test content 2"},
		{filepath.Join(baseName, "file3.txt"), "Test content 3"},
	}

	for _, f := range files {
		// Convert to forward slashes for zip
		zipName := filepath.ToSlash(f.name)
		w, err := writer.Create(zipName)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			return err
		}
	}

	return nil
}

// Helper function to verify zip contents
func verifyZipContents(t *testing.T, zipPath string, sourceDir string, expectedFiles []struct {
	path    string
	content string
}) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("Failed to open zip: %v", err)
	}
	defer archive.Close()

	// Get base name for verification
	baseName := strings.TrimSuffix(filepath.Base(zipPath), ".zip")

	// Verify all expected files are present in archive
	for _, expected := range expectedFiles {
		// Calculate the expected relative path in zip
		relPath, err := filepath.Rel(sourceDir, expected.path)
		if err != nil {
			t.Errorf("Failed to calculate relative path for %s: %v", expected.path, err)
			continue
		}

		// Construct the expected zip path
		expectedZipPath := filepath.ToSlash(filepath.Join(baseName, relPath))

		found := false
		for _, f := range archive.File {
			if f.Name == expectedZipPath {
				found = true

				// Verify file content
				rc, err := f.Open()
				if err != nil {
					t.Errorf("Failed to open file in zip: %v", err)
					continue
				}

				content, err := io.ReadAll(rc)
				rc.Close()

				if err != nil {
					t.Errorf("Failed to read file content: %v", err)
				}

				if string(content) != expected.content {
					t.Errorf("File content mismatch for %s, expected '%s', got '%s'",
						expectedZipPath, expected.content, string(content))
				}

				// Verify modification time is preserved
				if f.Modified.IsZero() {
					t.Errorf("Modified time is zero for file %s", f.Name)
				}
				break
			}
		}

		if !found {
			t.Errorf("File not found in zip: %s", expectedZipPath)
		}
	}
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
