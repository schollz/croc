package croc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sender alone decides how many data connections carry chunks, so every
// combination of NoMultiplexing must finish, including a receiver that sets
// it against a multiplexing sender.
func TestNoMultiplexingCombinations(t *testing.T) {
	for _, tc := range []struct {
		name             string
		sender, receiver bool
	}{
		{"neither", false, false},
		{"sender only", true, false},
		{"receiver only", false, true},
		{"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 32 chunks, enough for the sender to hand some to every data connection.
			content := strings.Repeat("croc", 1<<18)
			dir, sendErr, receiveErr := promptTransfer(t,
				map[string]string{"payload.bin": content}, nil,
				Options{NoPrompt: true, NoMultiplexing: tc.sender},
				Options{NoPrompt: true, NoMultiplexing: tc.receiver})
			if sendErr != nil || receiveErr != nil {
				t.Fatalf("send: %v, receive: %v", sendErr, receiveErr)
			}
			got, err := os.ReadFile(filepath.Join(dir, "payload.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != content {
				t.Fatalf("received %d bytes, want %d", len(got), len(content))
			}
		})
	}
}
