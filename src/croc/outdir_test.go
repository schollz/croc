package croc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/utils"
)

func TestReceiveIntoOutDirWithoutChdir(t *testing.T) {
	single := map[string][]byte{"big.bin": bytes.Repeat([]byte("x"), 3<<20)}
	bundled := map[string][]byte{}
	for i := range 6 {
		bundled[fmt.Sprintf("small-%d.txt", i)] = bytes.Repeat([]byte{byte('a' + i)}, 100+i)
	}
	for name, files := range map[string]map[string][]byte{"single file": single, "bundle": bundled} {
		t.Run(name, func(t *testing.T) {
			relay := startTestRelay(t, 4)
			source, outDir := t.TempDir(), t.TempDir()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for n, content := range files {
				if err := os.WriteFile(filepath.Join(source, n), content, 0o644); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, filepath.Join(source, n))
			}
			filesInfo, folders, totalFolders, err := GetFilesInfo(paths, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			base := Options{
				SharedSecret: fmt.Sprintf("outdir-%d", time.Now().UnixNano()), RelayAddress: relay.address, RelayPassword: "pass123",
				DisableLocal: true, DisableClipboard: true, NoPrompt: true, Curve: "siec",
			}
			sendOptions, receiveOptions := base, base
			sendOptions.IsSender, sendOptions.Transport, sendOptions.RelayPorts = true, TransportRelay, slices.Clone(relay.allPorts)
			receiveOptions.OutDir = outDir
			s, err := newTestClient(sendOptions)
			if err != nil {
				t.Fatal(err)
			}
			r, err := newTestClient(receiveOptions)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cancelClients(s, r) })
			results := make(chan error, 2)
			go func() { results <- s.Send(filesInfo, folders, totalFolders) }()
			go func() { results <- r.Receive() }()
			timeout := time.After(20 * time.Second)
			for range 2 {
				select {
				case err := <-results:
					if err != nil {
						t.Fatal(err)
					}
				case <-timeout:
					t.Fatal("transfer timed out")
				}
			}
			for n, want := range files {
				got, err := os.ReadFile(filepath.Join(outDir, n))
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("%s in OutDir: %d bytes, %v; want %d bytes", n, len(got), err, len(want))
				}
				if _, err := os.Stat(filepath.Join(cwd, n)); err == nil {
					t.Errorf("%s was written to the working directory", n)
				}
			}
			if now, _ := os.Getwd(); now != cwd {
				t.Errorf("working directory changed to %s", now)
			}
		})
	}
}

// swapOutDir opens c's receive root on outDir, then moves outDir aside and
// puts a new folder holding the given files at its path.
func swapOutDir(t *testing.T, c *Client, outDir string, files map[string][]byte) {
	t.Helper()
	if _, err := c.receiveFilesystem(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outDir, outDir+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, n), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A resume scans the file croc opened for writing, not whatever is at the
// path now: a folder moved into OutDir's place is neither scanned nor written.
func TestResumeScansOpenDestinationAfterOutDirSwap(t *testing.T) {
	const chunk = models.TCP_BUFFER_SIZE / 2
	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := append(bytes.Repeat([]byte{7}, 2*chunk), make([]byte, 2*chunk)...)
	if err := os.WriteFile(filepath.Join(outDir, "big.bin"), partial, 0o644); err != nil {
		t.Fatal(err)
	}
	c := overwriteTestClient(t, FileInfo{Name: "big.bin", FolderRemote: ".", Size: int64(len(partial))}, Options{OutDir: outDir})
	swapOutDir(t, c, outDir, map[string][]byte{"big.bin": make([]byte, len(partial))})

	if err := c.recipientInitializeFile(); err != nil {
		t.Fatal(err)
	}
	if want := []int64{chunk, 2 * chunk, 2}; !slices.Equal(c.CurrentFileChunkRanges, want) {
		t.Errorf("missing chunks = %v, want %v (the moved-aside file's)", c.CurrentFileChunkRanges, want)
	}
	if _, err := c.CurrentFile.WriteAt([]byte{9}, 3*chunk); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(outDir, "big.bin")); !bytes.Equal(got, make([]byte, len(partial))) {
		t.Error("the swapped-in file was written")
	}
}

// The unchanged-file check hashes the destination through the receive root.
func TestExistingDestinationHashUsesReceiveRootAfterOutDirSwap(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "same.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := utils.HashFile(filepath.Join(outDir, "same.txt"), defaultHashAlgorithm)
	if err != nil {
		t.Fatal(err)
	}
	c := overwriteTestClient(t, FileInfo{Name: "same.txt", FolderRemote: ".", Size: 4, Hash: hash},
		Options{OutDir: outDir, HashAlgorithm: defaultHashAlgorithm})
	swapOutDir(t, c, outDir, map[string][]byte{"same.txt": []byte("diff")})

	decision, err := c.preflightReceiveFile(0)
	if err != nil {
		t.Fatal(err)
	}
	if decision.requested {
		t.Errorf("decision = %+v, want the unchanged file skipped", decision)
	}
}
