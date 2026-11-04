package receivefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func shortNameOf(t *testing.T, target string) string {
	t.Helper()
	long, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	required, err := windows.GetShortPathName(long, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, required)
	n, err := windows.GetShortPathName(long, &buffer[0], required)
	if err != nil {
		t.Fatal(err)
	}
	if n >= required {
		t.Fatal("short path outgrew its buffer")
	}
	return filepath.Base(windows.UTF16ToString(buffer[:n]))
}

func TestShortNameCannotReachForbiddenDirectory(t *testing.T) {
	for _, forbidden := range []string{".ssh", ".gnupg"} {
		t.Run(forbidden, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, forbidden)
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			short := shortNameOf(t, target)
			if strings.EqualFold(short, forbidden) {
				t.Skip("8.3 name generation is unavailable on this volume")
			}
			root, err := OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			name := short + "/fixture.txt"
			if _, err := Normalize(name, false); !errors.Is(err, ErrSensitivePath) {
				t.Errorf("Normalize(%q) error = %v, want ErrSensitivePath", name, err)
			}
			file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, ErrSensitivePath) {
				t.Errorf("OpenFile(%q) error = %v, want ErrSensitivePath", name, err)
			}
			if _, err := os.Stat(filepath.Join(target, "fixture.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("forbidden destination was not left absent: %v", err)
			}
		})
	}
}
