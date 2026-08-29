package croc

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestReceiveHashAlgorithmAllowlist(t *testing.T) {
	for _, algorithm := range []string{"", "xxhash", "md5", "highway", "imohash"} {
		t.Run(algorithm, func(t *testing.T) {
			got, err := receiveHashAlgorithm(algorithm, false)
			if err != nil {
				t.Fatal(err)
			}
			want := algorithm
			if want == "" {
				want = defaultHashAlgorithm
			}
			if got != want {
				t.Fatalf("receiveHashAlgorithm(%q) = %q, want %q", algorithm, got, want)
			}
		})
	}

	if _, err := receiveHashAlgorithm(progressiveHashAlgorithm, false); err == nil || !strings.Contains(err.Error(), "without negotiating") {
		t.Fatalf("unnegotiated progressive hash error = %v", err)
	}
	if got, err := receiveHashAlgorithm(progressiveHashAlgorithm, true); err != nil || got != progressiveHashAlgorithm {
		t.Fatalf("negotiated progressive hash = %q, %v", got, err)
	}
	if _, err := receiveHashAlgorithm("not-a-real-hash", false); err == nil {
		t.Fatal("unknown hash algorithm was accepted")
	}
}

func TestProcessSenderInfoRejectsUnknownHashAlgorithm(t *testing.T) {
	client := &Client{Options: Options{NoPrompt: true}}
	done, err := client.processSenderInfo(SenderInfo{
		FilesToTransfer: []FileInfo{{Name: "payload", FolderRemote: ".", Size: 1}},
		HashAlgorithm:   "not-a-real-hash",
	})
	if !done || err == nil {
		t.Fatalf("processSenderInfo() = (%v, %v), want rejection", done, err)
	}
	if client.lifecycleSnapshot().FileInfoTransferred {
		t.Fatal("unknown hash algorithm finalized file metadata")
	}
}

func TestExistingDestinationHashFailureIsFatal(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("payload", []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("hash failed")
	originalHash := receiveFileHash
	receiveFileHash = func(string, string, ...bool) ([]byte, error) { return nil, wantErr }
	t.Cleanup(func() { receiveFileHash = originalHash })

	client := &Client{
		Options:          Options{HashAlgorithm: defaultHashAlgorithm},
		FilesToTransfer:  []FileInfo{{Name: "payload", FolderRemote: ".", Size: 4, Hash: []byte("different")}},
		FilesHasFinished: make(map[int]struct{}),
	}
	client.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
	defer client.closeReceiveFilesystem()

	err := client.updateIfRecipientHasFileInfo()
	if !errors.Is(err, wantErr) {
		t.Fatalf("updateIfRecipientHasFileInfo() error = %v, want %v", err, wantErr)
	}
}
