package croc

import (
	"io"
	"net"
	"os"
	"testing"

	"github.com/schollz/croc/v11/src/comm"
)

func overwriteTestClient(t *testing.T, file FileInfo, options Options) *Client {
	t.Helper()
	local, remote := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, remote)
	}()
	t.Cleanup(func() {
		_ = local.Close()
		_ = remote.Close()
	})
	client := &Client{
		Options:               options,
		FilesToTransfer:       []FileInfo{file},
		FilesHasFinished:      make(map[int]struct{}),
		TotalNumberOfContents: 1,
		conn:                  []*comm.Comm{comm.New(local)},
	}
	client.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
	t.Cleanup(client.closeReceiveFilesystem)
	return client
}

func TestSizeZeroOverwriteRequiresApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".bashrc", []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInput := receiveOverwriteInput
	receiveOverwriteInput = func(string) (string, error) { return "n", nil }
	t.Cleanup(func() { receiveOverwriteInput = originalInput })

	client := overwriteTestClient(t, FileInfo{Name: ".bashrc", FolderRemote: ".", Size: 0}, Options{
		HashAlgorithm: defaultHashAlgorithm,
		NoPrompt:      true,
	})
	if err := client.updateIfRecipientHasFileInfo(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(".bashrc")
	if err != nil || string(contents) != "keep me" {
		t.Fatalf("declined overwrite contents = %q, %v", contents, err)
	}
}

func TestSizeZeroOverwriteFlagTruncates(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".bashrc", []byte("replace me"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := overwriteTestClient(t, FileInfo{Name: ".bashrc", FolderRemote: ".", Size: 0}, Options{
		HashAlgorithm: defaultHashAlgorithm,
		Overwrite:     true,
	})
	if err := client.updateIfRecipientHasFileInfo(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(".bashrc")
	if err != nil || info.Size() != 0 {
		t.Fatalf("overwritten file info = %+v, %v", info, err)
	}
}

func TestIncomingSymlinkOverwriteRequiresApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("destination", []byte("regular file"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInput := receiveOverwriteInput
	receiveOverwriteInput = func(string) (string, error) { return "n", nil }
	t.Cleanup(func() { receiveOverwriteInput = originalInput })

	client := overwriteTestClient(t, FileInfo{Name: "destination", FolderRemote: ".", Symlink: "target"}, Options{HashAlgorithm: defaultHashAlgorithm})
	if err := client.updateIfRecipientHasFileInfo(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat("destination")
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("declined symlink overwrite info = %+v, %v", info, err)
	}
}

func TestNewSizeZeroFileDoesNotPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	originalInput := receiveOverwriteInput
	receiveOverwriteInput = func(string) (string, error) {
		t.Fatal("overwrite prompt called for new file")
		return "", nil
	}
	t.Cleanup(func() { receiveOverwriteInput = originalInput })

	client := overwriteTestClient(t, FileInfo{Name: "empty", FolderRemote: ".", Size: 0}, Options{HashAlgorithm: defaultHashAlgorithm})
	if err := client.updateIfRecipientHasFileInfo(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("empty")
	if err != nil || info.Size() != 0 {
		t.Fatalf("new empty file info = %+v, %v", info, err)
	}
}
