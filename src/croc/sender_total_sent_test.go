package croc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The sender resets TotalSent when it starts the next file while a chunk
// goroutine of the previous file may still add to it. The overlap is brief, so
// repeat the send for -race to catch it. Fewer than bundle.MinFiles files, so
// each one goes through the per-file path.
func TestMultiFileSendTotalSent(t *testing.T) {
	files := map[string]string{}
	for i := range 3 {
		files[fmt.Sprintf("file%d.txt", i)] = "hello"
	}
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir, sendErr, receiveErr := promptTransfer(t, files, nil,
				Options{NoPrompt: true}, Options{NoPrompt: true})
			if sendErr != nil || receiveErr != nil {
				t.Fatalf("send: %v, receive: %v", sendErr, receiveErr)
			}
			for name, want := range files {
				if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
