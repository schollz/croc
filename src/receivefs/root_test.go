package receivefs

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestRootAtomicWriteAndRename(t *testing.T) {
	directory := t.TempDir()
	root, err := OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFileAtomic("nested/value.txt", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(directory, "nested", "value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("contents = %q", got)
	}
}

func TestRootWriteResistsParentReplacement(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("os.Root cannot guarantee race containment on js")
	}
	receiveDirectory := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(receiveDirectory, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(receiveDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Simulate replacement after metadata validation but before the write.
	if err = os.Rename(parent, filepath.Join(receiveDirectory, "checked-parent")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, parent); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if file, openErr := root.OpenFile("parent/escaped.txt", os.O_CREATE|os.O_WRONLY, 0o600); openErr == nil {
		file.Close()
		t.Fatal("write through replaced parent unexpectedly succeeded")
	}
	if _, err = os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was created: %v", err)
	}
}

func TestRootRenameReplacesFinalSymlinkNotTarget(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("symlink behavior differs on js")
	}
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	temp, tempName, err := root.CreateTemp(".", ".part-", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = temp.WriteString("replacement"); err != nil {
		t.Fatal(err)
	}
	if err = temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(directory, "final.txt")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err = root.Rename(tempName, "final.txt"); err != nil {
		t.Fatal(err)
	}
	outsideBytes, err := os.ReadFile(outside)
	if err != nil || string(outsideBytes) != "keep" {
		t.Fatalf("outside target changed: %q, %v", outsideBytes, err)
	}
}

func TestRootConcurrentFilesystemMutationCannotEscape(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("os.Root cannot guarantee race containment on js")
	}
	receiveDirectory := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(receiveDirectory, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(receiveDirectory, "symlink-probe")
	if err := os.Symlink(outside, probe); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(receiveDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	stop := make(chan struct{})
	var mutator sync.WaitGroup
	mutator.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.RemoveAll(parent)
			_ = os.Symlink(outside, parent)
			_ = os.Remove(parent)
			_ = os.Mkdir(parent, 0o700)
		}
	})
	for range 500 {
		_ = root.MkdirAll("parent/nested", 0o700)
		file, openErr := root.OpenFile("parent/payload.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if openErr == nil {
			_, _ = file.WriteString("contained")
			_ = file.Close()
		}
	}
	close(stop)
	mutator.Wait()

	for _, escaped := range []string{
		filepath.Join(outside, "payload.txt"),
		filepath.Join(outside, "nested"),
	} {
		if _, err = os.Stat(escaped); !os.IsNotExist(err) {
			t.Fatalf("filesystem race created %s outside root: %v", escaped, err)
		}
	}
}
