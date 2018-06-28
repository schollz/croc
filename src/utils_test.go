package croc

import (
	"os"
	"testing"
)

func TestSplitFile(t *testing.T) {
	err := splitFile("testing_data/README.md", 3)
	if err != nil {
		t.Error(err)
	}
	os.Remove("testing_data/README.md.0")
	os.Remove("testing_data/README.md.1")
}

func TestFileSize(t *testing.T) {
	t.Run("File is ok ", func(t *testing.T) {
		_, err := fileSize("testing_data/README.md")
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
	})
	t.Run("File does not exist", func(t *testing.T) {
		s, err := fileSize("testing_data/someStrangeFile")
		if err == nil {
			t.Error("should return an error")
		}
		if s != -1 {
			t.Errorf("size should be -1, got: %d", s)
		}
	})
}

func TestHashFile(t *testing.T) {
	t.Run("Hash created successfully", func(t *testing.T) {
		h, err := hashFile("testing_data/README.md")
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
		if len(h) != 32 {
			t.Errorf("invalid md5 hash, length should be 32 got: %d", len(h))
		}
	})
	t.Run("File does not exist", func(t *testing.T) {
		h, err := hashFile("testing_data/someStrangeFile")
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

func TestCopyFileContents(t *testing.T) {
	t.Run("Content copied successfully", func(t *testing.T) {
		f1 := "testing_data/README.md"
		f2 := "testing_data/CopyOfREADME.md"
		err := copyFileContents(f1, f2)
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
		f1Length, err := fileSize(f1)
		if err != nil {
			t.Errorf("can't get file nr1 size: %v", err)
		}
		f2Length, err := fileSize(f2)
		if err != nil {
			t.Errorf("can't get file nr2 size: %v", err)
		}

		if f1Length != f2Length {
			t.Errorf("size of both files should be same got: file1: %d file2: %d", f1Length, f2Length)
		}
		os.Remove(f2)
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("Files copied successfully", func(t *testing.T) {
		f1 := "testing_data/README.md"
		f2 := "testing_data/CopyOfREADME.md"
		err := copyFile(f1, f2)
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
		f1Length, err := fileSize(f1)
		if err != nil {
			t.Errorf("can't get file nr1 size: %v", err)
		}
		f2Length, err := fileSize(f2)
		if err != nil {
			t.Errorf("can't get file nr2 size: %v", err)
		}

		if f1Length != f2Length {
			t.Errorf("size of both files should be same got: file1: %d file2: %d", f1Length, f2Length)
		}
		os.Remove(f2)
	})
}

func TestCatFiles(t *testing.T) {
	t.Run("CatFiles passing", func(t *testing.T) {
		files := []string{"testing_data/catFile1.txt", "testing_data/catFile2.txt"}
		err := catFiles(files, "testing_data/CatFile.txt", false)
		if err != nil {
			t.Errorf("should pass with no error, got: %v", err)
		}
		if _, err := os.Stat("testing_data/CatFile.txt"); os.IsNotExist(err) {
			t.Errorf("file were not created: %v", err)
		}
		os.Remove("testing_data/CatFile.txt")
	})
}
