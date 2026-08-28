package croc

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/schollz/croc/v11/src/utils"
)

func progressiveTestFile(t *testing.T, directory, name string, data []byte) FileInfo {
	t.Helper()
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filename)
	if err != nil {
		t.Fatal(err)
	}
	return FileInfo{
		Name:         name,
		FolderRemote: "./",
		FolderSource: directory,
		Size:         info.Size(),
		ModTime:      info.ModTime(),
		Mode:         info.Mode(),
	}
}

func progressiveTestClient() *Client {
	return &Client{
		Options:          Options{IsSender: true, HashAlgorithm: "imohash", NoCompress: true},
		stop:             newStop(context.Background()),
		exactHashPending: -1,
		exactHashResults: make(map[int]bool),
	}
}

func TestDefaultHashFallsBackToXXHashForLegacyPeer(t *testing.T) {
	directory := t.TempDir()
	file := progressiveTestFile(t, directory, "payload", bytes.Repeat([]byte("legacy"), 1024))
	client := progressiveTestClient()
	if err := client.sendCollectFiles([]FileInfo{file}); err != nil {
		t.Fatal(err)
	}
	if err := client.finalizeHashNegotiation(); err != nil {
		t.Fatal(err)
	}
	if client.Options.HashAlgorithm != "xxhash" {
		t.Fatalf("wire algorithm = %q", client.Options.HashAlgorithm)
	}
	want, err := utils.HashFile(filepath.Join(directory, file.Name), "xxhash")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(client.FilesToTransfer[0].Hash, want) {
		t.Fatal("legacy fallback did not eagerly compute xxhash")
	}
}

func TestDefaultHashNegotiatesV2AndPreparesLazily(t *testing.T) {
	directory := t.TempDir()
	files := []FileInfo{
		progressiveTestFile(t, directory, "first", bytes.Repeat([]byte("first"), 1024)),
		progressiveTestFile(t, directory, "second", bytes.Repeat([]byte("second"), 1024)),
	}
	client := progressiveTestClient()
	if err := client.sendCollectFiles(files); err != nil {
		t.Fatal(err)
	}
	if !client.FilesToTransfer[0].Prepared || client.FilesToTransfer[1].Prepared {
		t.Fatalf("prepared flags = %v, %v", client.FilesToTransfer[0].Prepared, client.FilesToTransfer[1].Prepared)
	}
	client.peerProgressiveHash = true
	if err := client.finalizeHashNegotiation(); err != nil {
		t.Fatal(err)
	}
	if client.Options.HashAlgorithm != "imohash-v2" {
		t.Fatalf("wire algorithm = %q", client.Options.HashAlgorithm)
	}
	if client.FilesToTransfer[1].Prepared {
		t.Fatal("negotiation eagerly prepared a later file")
	}
}

func TestPreparedSourceMutationIsRejected(t *testing.T) {
	directory := t.TempDir()
	file := progressiveTestFile(t, directory, "payload", []byte("before"))
	client := progressiveTestClient()
	if err := client.sendCollectFiles([]FileInfo{file}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, file.Name), []byte("after mutation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.validateSourceUnchanged(0); err == nil {
		t.Fatal("mutated source was accepted")
	}
}

func TestProgressiveHashPreservesSymlinkSemantics(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink setup is platform dependent")
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, "link")
	if err := os.Symlink("target", filename); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filename)
	if err != nil {
		t.Fatal(err)
	}
	client := progressiveTestClient()
	file := FileInfo{Name: "link", FolderSource: directory, FolderRemote: "./", Size: info.Size(), ModTime: info.ModTime(), Mode: info.Mode()}
	if err := client.sendCollectFiles([]FileInfo{file}); err != nil {
		t.Fatal(err)
	}
	if !client.FilesToTransfer[0].Prepared || client.FilesToTransfer[0].Symlink != "target" {
		t.Fatalf("symlink metadata = %+v", client.FilesToTransfer[0])
	}
}

func TestExactHashDecisionIsConsumed(t *testing.T) {
	client := progressiveTestClient()
	client.exactHashResults[3] = false
	known, matches, pending := client.exactHashDecision(3)
	if !known || matches || pending {
		t.Fatalf("decision = known:%v matches:%v pending:%v", known, matches, pending)
	}
	known, _, _ = client.exactHashDecision(3)
	if known {
		t.Fatal("exact decision was not consumed")
	}
}
