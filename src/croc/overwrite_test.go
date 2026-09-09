package croc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"sync"
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
		receiveMutex:          new(sync.Mutex),
	}
	client.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
	t.Cleanup(client.closeReceiveFilesystem)
	t.Cleanup(func() {
		if client.CurrentFile != nil {
			_ = client.CurrentFile.Close()
		}
	})
	return client
}

func TestReceiveOverwritePromptHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	overwrite, err := askReceiveOverwrite(ctx, FileInfo{Name: "payload", FolderRemote: "."}, false)
	if overwrite || !errors.Is(err, context.Canceled) {
		t.Fatalf("askReceiveOverwrite() = (%v, %v), want (false, context.Canceled)", overwrite, err)
	}
}

func TestSizeZeroOverwriteRequiresApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".bashrc", []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInput := receiveOverwriteInput
	receiveOverwriteInput = func(context.Context, string) (string, error) { return "n", nil }
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
	receiveOverwriteInput = func(context.Context, string) (string, error) { return "n", nil }
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
	receiveOverwriteInput = func(context.Context, string) (string, error) {
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

func TestReceiveStdinNamedFileCollision(t *testing.T) {
	const name = "croc-stdin-notes.txt"
	const original = "keep me"
	files := []struct {
		name    string
		size    int64
		symlink string
	}{
		{name: "same size", size: int64(len(original))},
		{name: "shorter", size: 3},
		{name: "longer", size: 12},
		{name: "empty"},
		{name: "symlink", size: 6, symlink: "target"},
	}
	policies := []struct {
		name        string
		answer      string
		inputErr    error
		overwrite   bool
		rename      bool
		wantPrompts int
		wantReceive bool
	}{
		{name: "declined", answer: "n", wantPrompts: 1},
		{name: "closed stdin", inputErr: io.EOF, wantPrompts: 1},
		{name: "approved", answer: "yes", wantPrompts: 1, wantReceive: true},
		{name: "overwrite", overwrite: true, wantReceive: true},
		{name: "rename", rename: true, wantReceive: true},
	}
	for _, file := range files {
		for _, policy := range policies {
			t.Run(file.name+"/"+policy.name, func(t *testing.T) {
				t.Chdir(t.TempDir())
				if err := os.WriteFile(name, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
				promptCount := 0
				originalInput := receiveOverwriteInput
				receiveOverwriteInput = func(context.Context, string) (string, error) {
					promptCount++
					return policy.answer, policy.inputErr
				}
				t.Cleanup(func() { receiveOverwriteInput = originalInput })

				client := overwriteTestClient(t, FileInfo{
					Name: name, FolderRemote: ".", Size: file.size, Symlink: file.symlink,
					Hash: []byte("different"), Mode: 0o644,
				}, Options{
					HashAlgorithm: defaultHashAlgorithm,
					NoPrompt:      true,
					Overwrite:     policy.overwrite,
					Rename:        policy.rename,
				})
				if err := client.updateIfRecipientHasFileInfo(); err != nil {
					t.Fatal(err)
				}
				if promptCount != policy.wantPrompts {
					t.Errorf("overwrite prompts = %d, want %d", promptCount, policy.wantPrompts)
				}
				if !policy.wantReceive || policy.rename {
					info, err := os.Lstat(name)
					if err != nil || !info.Mode().IsRegular() {
						t.Fatalf("original file replaced: info = %+v, err = %v", info, err)
					}
					contents, err := os.ReadFile(name)
					if err != nil || string(contents) != original {
						t.Fatalf("original contents = %q, %v, want %q", contents, err, original)
					}
				}
				if !policy.wantReceive {
					if client.CurrentFile != nil || client.lifecycleSnapshot().RecipientRequested {
						t.Fatal("declined file was opened or requested")
					}
					return
				}

				wantName := name
				if policy.rename {
					wantName = "croc-stdin-notes (1).txt"
				}
				if got := client.FilesToTransfer[0].Name; got != wantName {
					t.Fatalf("received filename = %q, want %q", got, wantName)
				}
				info, err := os.Lstat(wantName)
				if err != nil {
					t.Fatal(err)
				}
				if file.symlink != "" {
					target, err := os.Readlink(wantName)
					if err != nil || target != file.symlink {
						t.Fatalf("received symlink target = %q, %v, want %q", target, err, file.symlink)
					}
				} else {
					if !info.Mode().IsRegular() || info.Size() != file.size {
						t.Fatalf("received file info = %+v, want regular file of size %d", info, file.size)
					}
					if file.size > 0 && (client.CurrentFile == nil || !client.lifecycleSnapshot().RecipientRequested) {
						t.Fatal("approved file was not opened and requested")
					}
				}
			})
		}
	}
}

func TestReceiveTextArtifactDoesNotPromptOrRename(t *testing.T) {
	for _, policy := range []struct {
		name   string
		rename bool
	}{
		{name: "default"},
		{name: "rename", rename: true},
	} {
		t.Run(policy.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			const senderName = "croc-stdin-honest"
			const original = "keep me"
			if err := os.WriteFile(senderName, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			originalInput := receiveOverwriteInput
			receiveOverwriteInput = func(context.Context, string) (string, error) {
				t.Fatal("overwrite prompt called for receiver-created text artifact")
				return "", nil
			}
			t.Cleanup(func() { receiveOverwriteInput = originalInput })

			client := overwriteTestClient(t, FileInfo{}, Options{NoPrompt: true, Rename: policy.rename})
			client.resetLifecycle()
			done, err := client.processSenderInfo(SenderInfo{
				FilesToTransfer: []FileInfo{{Name: senderName, FolderRemote: ".", Size: 12}},
				SendingText:     true,
			})
			if done || err != nil {
				t.Fatalf("processSenderInfo() = (%v, %v), want accepted text offer", done, err)
			}
			artifactName := path.Base(client.FilesToTransfer[0].Name)
			if artifactName == senderName {
				t.Fatal("text transfer reused the sender's filename")
			}
			if err := client.updateIfRecipientHasFileInfo(); err != nil {
				t.Fatal(err)
			}
			if client.FilesToTransfer[0].Name != artifactName {
				t.Fatal("text artifact was renamed")
			}
			if client.CurrentFile == nil || !client.lifecycleSnapshot().RecipientRequested {
				t.Fatal("text artifact was not opened and requested")
			}
			contents, err := os.ReadFile(senderName)
			if err != nil || string(contents) != original {
				t.Fatalf("original contents = %q, %v, want %q", contents, err, original)
			}
		})
	}
}
