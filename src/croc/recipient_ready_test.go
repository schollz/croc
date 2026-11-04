package croc

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/schollz/croc/v11/src/message"
)

func recipientReadyTestClient(t *testing.T, files int) *Client {
	t.Helper()
	c := &Client{
		Options:         Options{IsSender: true},
		Key:             bytes.Repeat([]byte{0x42}, 32),
		stop:            newStop(t.Context()),
		FilesToTransfer: make([]FileInfo, files),
	}
	for i := range c.FilesToTransfer {
		c.FilesToTransfer[i].Size = 4
	}
	// Keep dispatch tests from starting file preparation or data transfer.
	c.updateLifecycle(func(state *transferLifecycle) {
		state.ChannelSecured = true
		state.FileInfoTransferred = true
		state.FileTransferred = true
	})
	t.Cleanup(c.stop.Cancel)
	return c
}

func processRecipientReadyForTest(t *testing.T, c *Client, request RemoteFileRequest) error {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := message.Encode(c.Key, message.Message{Type: message.TypeRecipientReady, Bytes: body})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.processMessage(payload, new(transferAttemptState))
	return err
}

func TestRecipientReadyRejectsInvalidFileIndex(t *testing.T) {
	for _, tc := range []struct {
		name         string
		index, files int
	}{
		{"negative", -1, 1},
		{"equal to length", 1, 1},
		{"far beyond length", 1 << 30, 3},
		{"empty manifest", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := recipientReadyTestClient(t, tc.files)
			c.Options.Ask = true
			c.FilesToTransferCurrentNum = 2
			c.CurrentFileChunkRanges = []int64{2, 3}
			c.CurrentFileChunkCount = 3
			c.peerReconnectVersion = 7
			c.peerInlineMetadata = true
			c.ExternalIPConnected = "198.51.100.1"
			before := c.lifecycleSnapshot()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("invalid peer index caused a panic: %v", r)
				}
			}()
			err := processRecipientReadyForTest(t, c, RemoteFileRequest{
				FilesToTransferCurrentNum: tc.index,
				ReconnectVersion:          8,
				Features:                  []string{perFileCompressionFeature},
				ExternalIP:                "198.51.100.2",
			})
			if err == nil {
				t.Fatal("invalid peer index was accepted")
			}
			if c.FilesToTransferCurrentNum != 2 || c.CurrentFileChunkCount != 3 || !slices.Equal(c.CurrentFileChunkRanges, []int64{2, 3}) {
				t.Fatal("rejected request changed file state")
			}
			if c.peerReconnectVersion != 7 || c.peerPerFileCompression || c.ExternalIPConnected != "198.51.100.1" {
				t.Fatal("rejected request changed peer metadata")
			}
			if c.lifecycleSnapshot() != before || c.activeTransferStarted() {
				t.Fatal("rejected request changed transfer lifecycle")
			}
		})
	}
}

func TestRecipientReadyAcceptsEveryValidFileIndex(t *testing.T) {
	for index := range 3 {
		c := recipientReadyTestClient(t, 3)
		if err := processRecipientReadyForTest(t, c, RemoteFileRequest{FilesToTransferCurrentNum: index}); err != nil {
			t.Fatalf("valid index %d was rejected: %v", index, err)
		}
		if c.FilesToTransferCurrentNum != index || !c.lifecycleSnapshot().RecipientRequested {
			t.Fatalf("valid index %d did not update transfer state", index)
		}
	}
}
