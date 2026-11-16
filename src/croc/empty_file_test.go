package croc

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestCreateEmptyFilePreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not supported on Windows")
	}
	for _, mode := range []os.FileMode{0o600, 0o750, 0o640} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			c := bundleTestClient(t)
			t.Chdir(t.TempDir())
			c.Options.IsSender = false
			fileInfo := FileInfo{Name: "empty", FolderRemote: ".", Size: 0, Mode: mode}
			c.FilesToTransfer = []FileInfo{fileInfo}
			if err := c.createEmptyFileAndFinish(fileInfo, 0); err != nil {
				t.Fatal(err)
			}
			stat, err := os.Stat(fileInfo.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got := stat.Mode().Perm(); got != mode {
				t.Fatalf("received empty file permissions = %#o, want %#o", got, mode)
			}
		})
	}
}
