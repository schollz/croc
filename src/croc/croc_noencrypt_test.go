package croc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCrocNoEncrypt transfers a file with --no-encrypt, with and without
// compression, since the compress and encrypt branches of sendData/receiveData
// are independent.
func TestCrocNoEncrypt(t *testing.T) {
	for _, noCompress := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-compress=%v", noCompress), func(t *testing.T) {
			testDir := t.TempDir()
			sourceDir := filepath.Join(testDir, "source")
			receiveDir := filepath.Join(testDir, "receive")
			for _, dir := range []string{sourceDir, receiveDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			const fileName = "payload.bin"
			want := bytes.Repeat([]byte("croc no-encrypt payload "), 100_000)
			sourcePath := filepath.Join(sourceDir, fileName)
			if err := os.WriteFile(sourcePath, want, 0o644); err != nil {
				t.Fatalf("write source file: %v", err)
			}

			filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{sourcePath}, false, false, nil)
			if err != nil {
				t.Fatalf("get source file info: %v", err)
			}

			originalCwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			if err := os.Chdir(receiveDir); err != nil {
				t.Fatalf("change to receive directory: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(originalCwd); err != nil {
					t.Errorf("restore working directory: %v", err)
				}
			})

			secret := fmt.Sprintf("no-encrypt-%d", time.Now().UnixNano())
			sender, err := New(Options{
				IsSender:      true,
				SharedSecret:  secret,
				RelayAddress:  "127.0.0.1:8281",
				RelayPorts:    []string{"8281"},
				RelayPassword: "pass123",
				NoPrompt:      true,
				DisableLocal:  true,
				Curve:         "siec",
				Overwrite:     true,
				NoCompress:    noCompress,
				NoEncrypt:     true,
			})
			if err != nil {
				t.Fatalf("create sender: %v", err)
			}
			// the receiver learns NoEncrypt from the sender's (encrypted) fileinfo;
			// its own NoEncrypt is only a pre-consent, so cover both values
			receiver, err := New(Options{
				IsSender:      false,
				SharedSecret:  secret,
				RelayAddress:  "127.0.0.1:8281",
				RelayPassword: "pass123",
				NoPrompt:      true,
				DisableLocal:  true,
				Curve:         "siec",
				Overwrite:     true,
				NoEncrypt:     noCompress,
			})
			if err != nil {
				t.Fatalf("create receiver: %v", err)
			}

			errCh := make(chan error, 2)
			go func() {
				errCh <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
			}()
			time.Sleep(100 * time.Millisecond)
			go func() {
				errCh <- receiver.Receive()
			}()
			for i := 0; i < 2; i++ {
				if err := <-errCh; err != nil {
					t.Errorf("transfer failed: %v", err)
				}
			}

			if !receiver.Options.NoEncrypt {
				t.Error("receiver did not adopt the sender's NoEncrypt option")
			}
			got, err := os.ReadFile(filepath.Join(receiveDir, fileName))
			if err != nil {
				t.Fatalf("read received file: %v", err)
			}
			if !bytes.Equal(want, got) {
				t.Error("received payload does not match source")
			}
		})
	}
}
