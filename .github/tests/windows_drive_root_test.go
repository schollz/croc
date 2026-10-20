//go:build windows

package croc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/schollz/croc/v11/src/receivefs"
)

// This harness is copied into the reviewed source by the manual Windows check.
// A normal t.TempDir fixture cannot reproduce the drive-root prefix bug.
func TestWindowsDriveRootMetadata(t *testing.T) {
	volumeRoot := filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
	for _, fixture := range []struct {
		name     string
		atRoot   bool
		emptyTop bool
	}{
		{name: "drive-root-mixed", atRoot: true},
		{name: "drive-root-empty", atRoot: true, emptyTop: true},
		{name: "nested-mixed"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			parent := volumeRoot
			if !fixture.atRoot {
				parent = t.TempDir()
			}
			source, err := os.MkdirTemp(parent, "croc-path-check-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(source); err != nil {
					t.Error(err)
				}
			})
			base := filepath.Base(source)
			wantFiles := map[string]bool{}
			wantEmpty := map[string]bool{base + "/": true}
			if !fixture.emptyTop {
				wantEmpty = map[string]bool{base + "/empty/": true, base + "/deep/empty/": true}
				for _, folder := range []string{"empty", "deep/empty", "populated"} {
					if err := os.MkdirAll(filepath.Join(source, filepath.FromSlash(folder)), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{"file.txt", "populated/file.txt"} {
					if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte("x"), 0o644); err != nil {
						t.Fatal(err)
					}
					wantFiles[base+"/"+name] = true
				}
			}
			t.Logf("source: %s (drive root: %t)", source, fixture.atRoot)
			t.Chdir(parent)
			for _, input := range []struct{ name, path string }{
				{"absolute", source},
				{"relative", base},
				{"dot-relative-trailing-separator", `.\` + base + `\`},
			} {
				t.Run(input.name, func(t *testing.T) {
					files, empty, _, err := GetFilesInfo([]string{input.path}, false, false, nil)
					if err != nil {
						t.Fatal(err)
					}
					gotFiles, gotEmpty := map[string]bool{}, map[string]bool{}
					for _, file := range files {
						gotFiles[file.FolderRemote+file.Name] = true
					}
					for _, folder := range empty {
						gotEmpty[folder.FolderRemote] = true
					}
					if !reflect.DeepEqual(gotFiles, wantFiles) || !reflect.DeepEqual(gotEmpty, wantEmpty) {
						t.Fatalf("metadata: files=%v empty=%v; want files=%v empty=%v", gotFiles, gotEmpty, wantFiles, wantEmpty)
					}
					_, normalizedEmpty, err := validateReceiveMetadata(files, empty)
					if err != nil {
						t.Fatalf("receiver rejected metadata: %v", err)
					}
					root, err := receivefs.OpenRoot(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer root.Close()
					for _, folder := range normalizedEmpty {
						if err := root.MkdirAll(folder.FolderRemote, 0o755); err != nil {
							t.Fatalf("recreate empty folder: %v", err)
						}
						if info, err := root.Stat(folder.FolderRemote); err != nil || !info.IsDir() {
							t.Fatalf("empty folder missing: %s (%v)", folder.FolderRemote, err)
						}
					}
					if fixture.atRoot {
						// The old prefix ends in two separators at a drive root.
						legacy := strings.ReplaceAll(strings.TrimPrefix(source, filepath.Dir(source)+string(os.PathSeparator)), string(os.PathSeparator), "/") + "/"
						if _, _, err := validateReceiveMetadata(nil, []FileInfo{{FolderRemote: legacy}}); err == nil {
							t.Fatalf("negative control: receiver accepted old absolute path %q", legacy)
						}
					}
				})
			}
		})
	}
}
