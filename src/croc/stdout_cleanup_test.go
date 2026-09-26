package croc

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStdoutDoesNotDeleteUnwrittenLocalFile(t *testing.T) {
	const kept = "keep-this-local-copy\n"
	senderPayload := []byte("sender-bytes-that-differ\n")
	keptCopy := kept

	tests := []struct {
		name            string
		sender          []byte
		local           *string
		answer          string
		inputErr        error
		wantPrompts     int
		wantTransferred int
		wantUnchanged   int
		wantContents    *string
	}{
		{
			name:         "declined overwrite",
			sender:       senderPayload,
			local:        &keptCopy,
			answer:       "n",
			wantPrompts:  1,
			wantContents: &keptCopy,
		},
		{
			name:         "overwrite prompt eof",
			sender:       senderPayload,
			local:        &keptCopy,
			inputErr:     io.EOF,
			wantPrompts:  1,
			wantContents: &keptCopy,
		},
		{
			name:         "declined resume",
			sender:       senderPayload,
			local:        ptr(string(senderPayload[:8])),
			answer:       "n",
			wantPrompts:  1,
			wantContents: ptr(string(senderPayload[:8])),
		},
		{
			name:          "identical hash",
			sender:        senderPayload,
			local:         ptr(string(senderPayload)),
			wantUnchanged: 1,
			wantContents:  ptr(string(senderPayload)),
		},
		{
			name:            "approved overwrite is removed after stdout",
			sender:          senderPayload,
			local:           &keptCopy,
			answer:          "y",
			wantPrompts:     1,
			wantTransferred: 1,
		},
		{
			name:            "new file is staged and removed",
			sender:          senderPayload,
			wantTransferred: 1,
		},
		{
			name:            "new empty file is staged and removed",
			sender:          []byte{},
			wantTransferred: 1,
		},
		{
			name:         "declined empty overwrite",
			sender:       []byte{},
			local:        &keptCopy,
			answer:       "n",
			wantPrompts:  1,
			wantContents: &keptCopy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receiveDir := t.TempDir()
			sendDir := t.TempDir()
			payloadPath := filepath.Join(sendDir, ".bashrc")
			if err := os.WriteFile(payloadPath, tc.sender, 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.local != nil {
				if err := os.WriteFile(filepath.Join(receiveDir, ".bashrc"), []byte(*tc.local), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(receiveDir)

			prompts := 0
			originalInput := receiveOverwriteInput
			receiveOverwriteInput = func(context.Context, string) (string, error) {
				prompts++
				return tc.answer, tc.inputErr
			}
			t.Cleanup(func() { receiveOverwriteInput = originalInput })

			localPorts := freeConsecutiveTestPorts(t, 2)
			localAddress := net.JoinHostPort("127.0.0.1", localPorts[0])
			secret := fmt.Sprintf("8123-stdoutclean-%d", time.Now().UnixNano())
			sender, err := newTestClient(Options{
				IsSender:      true,
				SharedSecret:  secret,
				RelayAddress:  localAddress,
				RelayPorts:    append([]string(nil), localPorts...),
				RelayPassword: "pass123",
				NoPrompt:      true,
				DisableLocal:  false,
				OnlyLocal:     true,
				Curve:         "ed25519",
				GitIgnore:     false,
				NoCompress:    true,
			})
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := newTestClient(Options{
				IsSender:      false,
				SharedSecret:  secret,
				RelayAddress:  localAddress,
				IP:            localAddress,
				RelayPassword: "pass123",
				Stdout:        true,
				NoPrompt:      true,
				DisableLocal:  false,
				Curve:         "ed25519",
				NoCompress:    true,
			})
			if err != nil {
				t.Fatal(err)
			}

			filesInfo, emptyFolders, totalNumberFolders, err := GetFilesInfo([]string{payloadPath}, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			go func() {
				results <- sender.Send(filesInfo, emptyFolders, totalNumberFolders)
			}()
			waitForRelayPorts(t, localPorts)
			go func() {
				results <- receiver.Receive()
			}()
			waitForTransferResults(t, results, 2, 20*time.Second, sender, receiver)

			if prompts != tc.wantPrompts {
				t.Errorf("overwrite prompts = %d, want %d", prompts, tc.wantPrompts)
			}
			if receiver.numberOfTransferredFiles != tc.wantTransferred {
				t.Errorf("transferred files = %d, want %d", receiver.numberOfTransferredFiles, tc.wantTransferred)
			}
			// A finished non-empty receive is hashed again and counted unchanged.
			// Only the skip path must report the unchanged total exactly.
			if tc.wantTransferred == 0 && receiver.numberOfUnchangedFiles != tc.wantUnchanged {
				t.Errorf("unchanged files = %d, want %d", receiver.numberOfUnchangedFiles, tc.wantUnchanged)
			}

			got, err := os.ReadFile(filepath.Join(receiveDir, ".bashrc"))
			if tc.wantContents == nil {
				if err == nil {
					t.Fatalf("staging file still exists with contents %q", got)
				}
				if !os.IsNotExist(err) {
					t.Fatalf("stat staging file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("pre-existing .bashrc was removed: %v", err)
			}
			if string(got) != *tc.wantContents {
				t.Fatalf("pre-existing .bashrc contents = %q, want %q", got, *tc.wantContents)
			}
		})
	}
}

func ptr(value string) *string {
	return &value
}
