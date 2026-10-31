package receivefs

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
)

func TestPublicationCapabilityAndProbeCleanup(t *testing.T) {
	directory := t.TempDir()
	root, err := OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if supported, err := root.CanPublishNew("nested"); err != nil || !supported {
		t.Fatalf("native link probe: %v, %v", supported, err)
	}
	unsupported := error(syscall.EXDEV)
	if runtime.GOOS == "windows" {
		unsupported = syscall.Errno(17)
	}
	ioFailure := errors.New("destination I/O failure")
	for _, tc := range []struct {
		name      string
		cause     error
		wantError bool
	}{{"separate mount", unsupported, false}, {"I/O", ioFailure, true}, {"permissions", os.ErrPermission, true}} {
		t.Run(tc.name, func(t *testing.T) {
			supported, err := root.probePublishNew("nested", func(string, string) error { return &os.LinkError{Op: "link", Err: tc.cause} })
			if supported || (err != nil) != tc.wantError {
				t.Fatalf("probe: %v, %v", supported, err)
			}
			if tc.wantError && !errors.Is(err, tc.cause) {
				t.Fatal("lost cause", err)
			}
		})
	}
	for _, folder := range []string{directory, directory + "/nested"} {
		entries, err := os.ReadDir(folder)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Name() != "nested" {
				t.Fatal("probe leaked", entry.Name())
			}
		}
	}
}
