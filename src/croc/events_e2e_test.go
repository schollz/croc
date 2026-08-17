package croc

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/events"
	log "github.com/schollz/logger"
)

// TestJSONEventsE2E runs a full send/receive with JSON events enabled and
// verifies the stream contains version-agnostic phase, progress and
// completion events.
func TestJSONEventsE2E(t *testing.T) {
	log.SetLevel("trace")
	var wg sync.WaitGroup

	os.Remove("touched")
	os.Create("touched")
	defer os.Remove("touched")

	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-json-events",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8181",
		RelayPorts:    []string{"8181", "8182"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "ed25519",
		Overwrite:     true,
		Events:        true,
	})
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-json-events",
		Debug:         true,
		RelayAddress:  "127.0.0.1:8181",
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "ed25519",
		Overwrite:     true,
		Events:        true,
	})
	if err != nil {
		t.Fatalf("receiver: %v", err)
	}

	// Capture the event stream instead of stderr.
	var eventsBuf bytes.Buffer
	events.Enable(&eventsBuf)
	defer events.Enable(nil)

	wg.Add(2)
	go func() {
		defer wg.Done()
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{"../../LICENSE", "touched"}, false, false, []string{})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
			return
		}
		if err := sender.Send(filesInfo, emptyFolders, totalNumberFolders); err != nil {
			t.Errorf("send failed: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		defer wg.Done()
		if err := receiver.Receive(); err != nil {
			t.Errorf("receive failed: %v", err)
		}
	}()
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(eventsBuf.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected several event lines, got %d: %q", len(lines), eventsBuf.String())
	}

	var phases []string
	var progress events.ProgressEvent
	sawProgress := false
	var complete events.CompleteEvent
	sawComplete := false
	for _, line := range lines {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("invalid event line %q: %v", line, err)
		}
		switch raw["type"] {
		case "phase":
			phase, _ := raw["phase"].(string)
			phases = append(phases, phase)
		case "progress":
			if err := json.Unmarshal([]byte(line), &progress); err != nil {
				t.Fatalf("progress event %q: %v", line, err)
			}
			sawProgress = true
		case "complete":
			if err := json.Unmarshal([]byte(line), &complete); err != nil {
				t.Fatalf("complete event %q: %v", line, err)
			}
			sawComplete = true
		}
	}

	if !sawComplete || len(complete.Files) != 2 {
		t.Errorf("expected complete event with 2 files, got %+v (saw=%v)", complete, sawComplete)
	}

	// At least one progress event for a non-empty file, fully transferred.
	if !sawProgress {
		t.Error("expected at least one progress event")
	} else if progress.BytesTotal <= 0 {
		t.Errorf("progress bytesTotal = %d, want > 0", progress.BytesTotal)
	} else if progress.BytesTransferred > progress.BytesTotal {
		t.Errorf("progress bytesTransferred %d > bytesTotal %d", progress.BytesTransferred, progress.BytesTotal)
	} else if progress.Percent < 0 || progress.Percent > 100 {
		t.Errorf("progress percent = %v, want in [0,100]", progress.Percent)
	}

	// Both sides emit connecting and transferring; the sender emits hashing.
	for _, want := range []string{"connecting", "transferring"} {
		found := false
		for _, phase := range phases {
			if phase == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected phase %q in events, got %v", want, phases)
		}
	}
}
