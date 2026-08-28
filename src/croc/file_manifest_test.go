package croc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/pakekey"
)

const manifestTestFrameLimit = 64 * 1024 * 1024

var manifestTestKey = bytes.Repeat([]byte{0x42}, 32)

func manifestTestHeader(t *testing.T, fileCount, emptyFolderCount int) message.Message {
	t.Helper()
	payload, err := json.Marshal(fileInfoManifestHeader{
		FileCount:          fileCount,
		EmptyFolderCount:   emptyFolderCount,
		TotalNumberFolders: emptyFolderCount,
		HashAlgorithm:      defaultHashAlgorithm,
		ReconnectVersion:   ReconnectVersion,
		NextReconnectRoom:  "next-room",
		Features:           []string{perFileCompressionFeature},
	})
	if err != nil {
		t.Fatal(err)
	}
	return message.Message{Type: message.TypeFileInfoStart, Bytes: payload}
}

func manifestTestBatch(t *testing.T, kind string, entries ...FileInfo) []byte {
	t.Helper()
	payload, err := json.Marshal(fileInfoManifestBatch{Kind: kind, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func manifestTestClient() *Client {
	client := &Client{
		Options:             Options{NoPrompt: true},
		Key:                 append([]byte(nil), manifestTestKey...),
		peerChunkedFileInfo: true,
		FilesHasFinished:    make(map[int]struct{}),
	}
	client.updateLifecycle(func(state *transferLifecycle) { state.ChannelSecured = true })
	return client
}

func encryptedManifestRoundTrip(t *testing.T, key []byte, m message.Message) message.Message {
	t.Helper()
	encoded, err := message.Encode(key, m)
	if err != nil {
		t.Fatalf("encode %s: %v", m.Type, err)
	}
	if len(encoded) >= manifestTestFrameLimit {
		t.Fatalf("encoded %s frame = %d bytes; want less than %d", m.Type, len(encoded), manifestTestFrameLimit)
	}
	decoded, err := message.Decode(key, encoded)
	if err != nil {
		t.Fatalf("decode %s: %v", m.Type, err)
	}
	return decoded
}

func assertManifestCleared(t *testing.T, client *Client) {
	t.Helper()
	if client.incomingFileManifest != nil {
		t.Fatal("partial file manifest was retained after failure")
	}
}

func TestChunkedFileInfoFeatureAdvertised(t *testing.T) {
	features := (&Client{Options: Options{Transport: TransportRelay}}).pakeFeatures()
	if !supportsFeature(features, chunkedFileInfoFeature) {
		t.Fatalf("PAKE features = %v; want %q", features, chunkedFileInfoFeature)
	}
}

func TestChunkedFileInfoCapabilityNegotiatesThroughPAKE(t *testing.T) {
	for _, tt := range []struct {
		name     string
		features []string
		want     bool
	}{
		{name: "supported", features: []string{chunkedFileInfoFeature}, want: true},
		{name: "legacy", features: nil, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const (
				room  = "manifest-negotiation-room"
				curve = "p256"
			)
			initiator, err := pakekey.Init([]byte("manifest password"), 0, curve, pakekey.PurposeTransfer, room)
			if err != nil {
				t.Fatal(err)
			}
			local, peer := net.Pipe()
			defer local.Close()
			defer peer.Close()
			client := &Client{
				Options:             Options{IsSender: true, Transport: TransportRelay, RoomName: room},
				pakePassphrase:      "manifest password",
				conn:                []*comm.Comm{comm.New(local)},
				peerChunkedFileInfo: !tt.want,
			}
			received := make(chan error, 1)
			go func() {
				payload, receiveErr := comm.New(peer).Receive()
				if receiveErr == nil {
					_, receiveErr = message.Decode(nil, payload)
				}
				received <- receiveErr
			}()
			err = client.processMessagePake(message.Message{
				Type:     message.TypePAKE,
				Version:  pakekey.ProtocolVersion,
				Bytes:    initiator.Bytes(),
				Bytes2:   []byte(curve),
				Features: tt.features,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-received; err != nil {
				t.Fatal(err)
			}
			if client.peerChunkedFileInfo != tt.want {
				t.Fatalf("negotiated chunked file info = %v; want %v", client.peerChunkedFileInfo, tt.want)
			}
		})
	}
}

func TestEncodeFileInfoBatchesBoundedOrderedAndSanitized(t *testing.T) {
	files := make([]FileInfo, 12)
	for i := range files {
		files[i] = FileInfo{
			Name:         fmt.Sprintf("file-%02d-%s", i, strings.Repeat("x", 30)),
			FolderRemote: ".",
			FolderSource: "/private/source",
			Size:         int64(i + 1),
			IsIgnored:    true,
		}
	}

	var batches [][]byte
	err := encodeFileInfoBatches(files, fileInfoBatchKindFiles, 400, func(payload []byte) error {
		batches = append(batches, append([]byte(nil), payload...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) < 2 {
		t.Fatalf("encoded %d batches; want more than one", len(batches))
	}

	var got []FileInfo
	for i, payload := range batches {
		if len(payload) > 400 {
			t.Fatalf("batch %d size = %d; want <= 400", i, len(payload))
		}
		var batch fileInfoManifestBatch
		if err := json.Unmarshal(payload, &batch); err != nil {
			t.Fatalf("decode batch %d: %v", i, err)
		}
		if batch.Kind != fileInfoBatchKindFiles {
			t.Fatalf("batch %d kind = %q", i, batch.Kind)
		}
		got = append(got, batch.Entries...)
	}
	if len(got) != len(files) {
		t.Fatalf("decoded %d files; want %d", len(got), len(files))
	}
	for i, info := range got {
		if info.Name != files[i].Name {
			t.Fatalf("file %d name = %q; want %q", i, info.Name, files[i].Name)
		}
		if info.FolderSource != "" || info.IsIgnored {
			t.Fatalf("file %d retained sender-only metadata: %+v", i, info)
		}
	}
	if files[0].FolderSource == "" || !files[0].IsIgnored {
		t.Fatal("batch encoding mutated sender metadata")
	}
}

func TestEmitChunkedSenderInfoUsesOrderedBoundedFrames(t *testing.T) {
	info := SenderInfo{
		FilesToTransfer: []FileInfo{
			{Name: "a.txt", FolderRemote: ".", Prepared: true},
			{Name: "b.txt", FolderRemote: ".", Prepared: true},
		},
		EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "empty-a"}, {FolderRemote: "empty-b"}},
		HashAlgorithm:          defaultHashAlgorithm,
	}
	limits := fileInfoManifestLimits{maxBatchJSONSize: 100, maxEntries: 10, maxManifestJSONSize: 10_000}
	var frames []message.Message
	err := emitChunkedSenderInfo(info, limits, func(m message.Message) error {
		frames = append(frames, m)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 4 || frames[0].Type != message.TypeFileInfoStart || frames[len(frames)-1].Type != message.TypeFileInfoEnd {
		t.Fatalf("unexpected manifest frame sequence: %+v", frames)
	}
	for i, frame := range frames[1 : len(frames)-1] {
		if frame.Type != message.TypeFileInfoBatch || frame.Num != i {
			t.Fatalf("batch %d = %+v", i, frame)
		}
		if len(frame.Bytes) > limits.maxBatchJSONSize {
			t.Fatalf("batch %d size = %d", i, len(frame.Bytes))
		}
	}
	if got := frames[len(frames)-1].Num; got != len(frames)-2 {
		t.Fatalf("end sequence = %d; want %d", got, len(frames)-2)
	}
}

func TestEncodeFileInfoBatchesRejectsOversizedEntry(t *testing.T) {
	err := encodeFileInfoBatches(
		[]FileInfo{{Name: strings.Repeat("x", 128)}},
		fileInfoBatchKindFiles,
		64,
		func([]byte) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "single file metadata entry") {
		t.Fatalf("oversized entry error = %v", err)
	}
}

func TestEmitChunkedSenderInfoRejectsCumulativeAndEntryLimits(t *testing.T) {
	info := SenderInfo{
		FilesToTransfer: []FileInfo{{Name: "a.txt", FolderRemote: "."}},
		HashAlgorithm:   defaultHashAlgorithm,
	}
	headerPayload, err := json.Marshal(fileInfoManifestHeaderFromSenderInfo(info))
	if err != nil {
		t.Fatal(err)
	}
	limits := fileInfoManifestLimits{
		maxBatchJSONSize:    1024,
		maxEntries:          1,
		maxManifestJSONSize: int64(len(headerPayload) + 1),
	}
	var frames []message.Message
	err = emitChunkedSenderInfo(info, limits, func(m message.Message) error {
		frames = append(frames, m)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "metadata exceeds") {
		t.Fatalf("cumulative limit error = %v", err)
	}
	if len(frames) != 1 || frames[0].Type != message.TypeFileInfoStart {
		t.Fatalf("frames emitted before cumulative rejection = %+v", frames)
	}

	limits.maxManifestJSONSize = 10_000
	limits.maxEntries = 0
	err = emitChunkedSenderInfo(info, limits, func(message.Message) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "maximum combined entry count is 0") {
		t.Fatalf("entry limit error = %v", err)
	}
}

func TestNegotiatedSenderUsesEncryptedChunkedMessages(t *testing.T) {
	local, peer := net.Pipe()
	defer local.Close()
	defer peer.Close()
	client := &Client{
		Key:                 append([]byte(nil), manifestTestKey...),
		peerChunkedFileInfo: true,
		conn:                []*comm.Comm{comm.New(local)},
	}
	received := make(chan []message.Message, 1)
	errCh := make(chan error, 1)
	go func() {
		connection := comm.New(peer)
		var frames []message.Message
		for {
			payload, err := connection.Receive()
			if err != nil {
				errCh <- err
				return
			}
			decoded, err := message.Decode(manifestTestKey, payload)
			if err != nil {
				errCh <- err
				return
			}
			frames = append(frames, decoded)
			if decoded.Type == message.TypeFileInfoEnd {
				received <- frames
				return
			}
		}
	}()
	if err := client.sendSenderInfo(SenderInfo{
		FilesToTransfer: []FileInfo{{Name: "a.txt", FolderRemote: "."}},
		HashAlgorithm:   defaultHashAlgorithm,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case frames := <-received:
		if len(frames) != 3 {
			t.Fatalf("received %d chunked frames; want 3", len(frames))
		}
		for _, frame := range frames {
			if frame.Type == message.TypeFileInfo {
				t.Fatal("negotiated sender emitted legacy fileinfo")
			}
		}
	}
}

func TestChunkedFileInfoRoundTripFinalizesOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	client := manifestTestClient()
	client.peerProgressiveHash = true
	defer client.closeReceiveFilesystem()
	info := SenderInfo{
		FilesToTransfer: []FileInfo{
			{Name: "a.txt", FolderRemote: ".", FolderSource: "/source", Size: 1},
			{Name: "b.txt", FolderRemote: ".", Size: 2},
		},
		EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "empty", FolderSource: "/source", IsIgnored: true}},
		TotalNumberFolders:     1,
		HashAlgorithm:          progressiveHashAlgorithm,
		ReconnectVersion:       ReconnectVersion,
		NextReconnectRoom:      "next-room",
		Features:               []string{perFileCompressionFeature},
	}
	limits := fileInfoManifestLimits{maxBatchJSONSize: 180, maxEntries: 10, maxManifestJSONSize: 10_000}
	var batchCount int
	err := emitChunkedSenderInfo(info, limits, func(outbound message.Message) error {
		m := encryptedManifestRoundTrip(t, manifestTestKey, outbound)
		switch m.Type {
		case message.TypeFileInfoStart:
			if err := client.processMessageFileInfoStartWithLimits(m, limits); err != nil {
				return err
			}
			if client.peerReconnectVersion != ReconnectVersion || client.nextReconnectRoom != "next-room" {
				return errors.New("start header did not immediately apply reconnect metadata")
			}
			return nil
		case message.TypeFileInfoBatch:
			batchCount++
			return client.processMessageFileInfoBatch(m)
		case message.TypeFileInfoEnd:
			if client.lifecycleSnapshot().FileInfoTransferred || client.FilesToTransfer != nil {
				return errors.New("receiver finalized before fileinfo-end")
			}
			if _, statErr := os.Stat("empty"); !os.IsNotExist(statErr) {
				return fmt.Errorf("empty folder existed before end: %v", statErr)
			}
			_, endErr := client.processMessageFileInfoEnd(m)
			return endErr
		default:
			return fmt.Errorf("unexpected frame type %q", m.Type)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if batchCount < 2 {
		t.Fatalf("encoded %d batches; want multiple batches", batchCount)
	}
	if client.peerReconnectVersion != ReconnectVersion || client.nextReconnectRoom != "next-room" {
		t.Fatal("start header did not apply reconnect metadata")
	}
	if !client.lifecycleSnapshot().FileInfoTransferred {
		t.Fatal("file manifest did not finalize")
	}
	if len(client.FilesToTransfer) != 2 || client.FilesToTransfer[0].Name != "a.txt" || client.FilesToTransfer[1].Name != "b.txt" {
		t.Fatalf("received files = %+v", client.FilesToTransfer)
	}
	if client.FilesToTransfer[0].FolderSource != "" || client.EmptyFoldersToTransfer[0].FolderSource != "" || client.EmptyFoldersToTransfer[0].IsIgnored {
		t.Fatal("receiver retained sender-only metadata")
	}
	if _, err := os.Stat("empty"); err != nil {
		t.Fatalf("empty folder was not created after manifest finalization: %v", err)
	}

	preparedPayload, err := json.Marshal(filePrepared{Index: 1, Hash: []byte("prepared-hash"), IsCompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.processMessageFilePrepared(message.Message{Type: message.TypeFilePrepared, Bytes: preparedPayload}); err != nil {
		t.Fatal(err)
	}
	if client.FilesToTransfer[0].Prepared || !client.FilesToTransfer[1].Prepared || string(client.FilesToTransfer[1].Hash) != "prepared-hash" {
		t.Fatal("progressive preparation did not preserve the reconstructed file index")
	}
	if _, err = client.processMessageFileInfoEnd(message.Message{Type: message.TypeFileInfoEnd, Num: batchCount}); err == nil {
		t.Fatal("second fileinfo-end was accepted")
	}
	if _, err = client.processSenderInfo(info); err == nil || !strings.Contains(err.Error(), "already finalized") {
		t.Fatalf("duplicate finalization error = %v", err)
	}
}

func TestChunkedFileInfoEmptyManifest(t *testing.T) {
	t.Chdir(t.TempDir())
	client := manifestTestClient()
	defer client.closeReceiveFilesystem()
	err := emitChunkedSenderInfo(SenderInfo{HashAlgorithm: defaultHashAlgorithm}, defaultFileInfoManifestLimits, func(outbound message.Message) error {
		m := encryptedManifestRoundTrip(t, manifestTestKey, outbound)
		switch m.Type {
		case message.TypeFileInfoStart:
			return client.processMessageFileInfoStart(m)
		case message.TypeFileInfoBatch:
			return errors.New("empty manifest emitted a batch")
		case message.TypeFileInfoEnd:
			_, endErr := client.processMessageFileInfoEnd(m)
			return endErr
		default:
			return fmt.Errorf("unexpected frame type %q", m.Type)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	state := client.lifecycleSnapshot()
	if !state.FileInfoTransferred || len(client.FilesToTransfer) != 0 || len(client.EmptyFoldersToTransfer) != 0 {
		t.Fatalf("empty manifest lifecycle = %+v", state)
	}
}

func TestChunkedFileInfoRejectsInvalidSequencesAndLimits(t *testing.T) {
	fileBatch := manifestTestBatch(t, fileInfoBatchKindFiles, FileInfo{Name: "a", FolderRemote: "."})
	folderBatch := manifestTestBatch(t, fileInfoBatchKindEmptyFolders, FileInfo{FolderRemote: "empty"})

	t.Run("without negotiation", func(t *testing.T) {
		client := manifestTestClient()
		client.peerChunkedFileInfo = false
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err == nil {
			t.Fatal("unnegotiated manifest was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("before secure lifecycle", func(t *testing.T) {
		client := manifestTestClient()
		client.resetLifecycle()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err == nil {
			t.Fatal("manifest before secure lifecycle was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("without encryption key", func(t *testing.T) {
		client := manifestTestClient()
		client.Key = nil
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err == nil {
			t.Fatal("manifest without an encryption key was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("sender role", func(t *testing.T) {
		client := manifestTestClient()
		client.Options.IsSender = true
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err == nil {
			t.Fatal("sender accepted incoming manifest")
		}
		assertManifestCleared(t, client)
	})
	t.Run("malformed start", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(message.Message{Bytes: []byte("{")}); err == nil {
			t.Fatal("malformed start was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("batch before start", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: fileBatch}); err == nil {
			t.Fatal("batch before start was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("duplicate start", func(t *testing.T) {
		client := manifestTestClient()
		start := manifestTestHeader(t, 1, 0)
		if err := client.processMessageFileInfoStart(start); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoStart(start); err == nil {
			t.Fatal("duplicate start was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("skipped batch", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Num: 1, Bytes: fileBatch}); err == nil {
			t.Fatal("skipped batch was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("duplicate batch", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 2, 0)); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Num: 0, Bytes: fileBatch}); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Num: 0, Bytes: fileBatch}); err == nil {
			t.Fatal("duplicate batch was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("excess files", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 0, 0)); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: fileBatch}); err == nil {
			t.Fatal("excess files were accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("excess empty folders", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 0, 0)); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: folderBatch}); err == nil {
			t.Fatal("excess empty folders were accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("unknown kind", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err != nil {
			t.Fatal(err)
		}
		unknown := manifestTestBatch(t, "unknown", FileInfo{Name: "a", FolderRemote: "."})
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: unknown}); err == nil {
			t.Fatal("unknown batch kind was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("empty batch", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err != nil {
			t.Fatal(err)
		}
		empty := manifestTestBatch(t, fileInfoBatchKindFiles)
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: empty}); err == nil {
			t.Fatal("empty batch was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("batch limit", func(t *testing.T) {
		client := manifestTestClient()
		limits := fileInfoManifestLimits{maxBatchJSONSize: len(fileBatch) - 1, maxEntries: 10, maxManifestJSONSize: 1 << 20}
		if err := client.processMessageFileInfoStartWithLimits(manifestTestHeader(t, 1, 0), limits); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: fileBatch}); err == nil {
			t.Fatal("oversized batch was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("cumulative limit", func(t *testing.T) {
		client := manifestTestClient()
		start := manifestTestHeader(t, 1, 0)
		limits := fileInfoManifestLimits{
			maxBatchJSONSize:    1 << 20,
			maxEntries:          10,
			maxManifestJSONSize: int64(len(start.Bytes) + len(fileBatch) - 1),
		}
		if err := client.processMessageFileInfoStartWithLimits(start, limits); err != nil {
			t.Fatal(err)
		}
		if err := client.processMessageFileInfoBatch(message.Message{Bytes: fileBatch}); err == nil {
			t.Fatal("cumulative metadata overflow was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("start metadata limit", func(t *testing.T) {
		client := manifestTestClient()
		start := manifestTestHeader(t, 1, 0)
		limits := fileInfoManifestLimits{
			maxBatchJSONSize:    1 << 20,
			maxEntries:          10,
			maxManifestJSONSize: int64(len(start.Bytes) - 1),
		}
		if err := client.processMessageFileInfoStartWithLimits(start, limits); err == nil {
			t.Fatal("oversized start metadata was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("premature end", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 1, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.processMessageFileInfoEnd(message.Message{}); err == nil {
			t.Fatal("premature end was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("end sequence", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 0, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.processMessageFileInfoEnd(message.Message{Num: 1}); err == nil {
			t.Fatal("unexpected end sequence was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("end payload", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, 0, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.processMessageFileInfoEnd(message.Message{Bytes: []byte("unexpected")}); err == nil {
			t.Fatal("end payload was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("declared entry limit", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, maxChunkedManifestEntries, 1)); err == nil {
			t.Fatal("excess declared entries were accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("negative declared count", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, -1, 0)); err == nil {
			t.Fatal("negative declared entry count was accepted")
		}
		assertManifestCleared(t, client)
	})
	t.Run("untrusted count does not preallocate", func(t *testing.T) {
		client := manifestTestClient()
		if err := client.processMessageFileInfoStart(manifestTestHeader(t, maxChunkedManifestEntries, 0)); err != nil {
			t.Fatal(err)
		}
		if cap(client.incomingFileManifest.files) != 0 || cap(client.incomingFileManifest.emptyFolders) != 0 {
			t.Fatal("receiver preallocated directly from the declared manifest count")
		}
	})
}

func TestReconnectAndTerminalFailureClearPartialFileManifest(t *testing.T) {
	t.Run("reconnect", func(t *testing.T) {
		client := manifestTestClient()
		client.Options.IsSender = true
		client.Options.RoomName = "room"
		client.nextReconnectRoom = "next-room"
		client.incomingFileManifest = &incomingFileManifest{}
		client.peerChunkedFileInfo = true
		if err := client.resetForReconnectAttempt(1); err != nil {
			t.Fatal(err)
		}
		if client.incomingFileManifest != nil || client.peerChunkedFileInfo {
			t.Fatal("reconnect retained partial manifest or negotiated capability")
		}
	})
	t.Run("terminal failure", func(t *testing.T) {
		client := manifestTestClient()
		sentinel := errors.New("terminal setup failure")
		err := client.transferWithReconnect(func(int) error {
			client.incomingFileManifest = &incomingFileManifest{}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("terminal error = %v", err)
		}
		assertManifestCleared(t, client)
	})
}

func TestLegacyFileInfoRemainsCompatibleAndSanitized(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	client := &Client{conn: []*comm.Comm{comm.New(left)}}
	received := make(chan message.Message, 1)
	errCh := make(chan error, 1)
	go func() {
		payload, err := comm.New(right).Receive()
		if err != nil {
			errCh <- err
			return
		}
		decoded, err := message.Decode(nil, payload)
		if err != nil {
			errCh <- err
			return
		}
		received <- decoded
	}()

	err := client.sendSenderInfo(SenderInfo{FilesToTransfer: []FileInfo{{
		Name: "a", FolderRemote: ".", FolderSource: "/private", IsIgnored: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case got := <-received:
		if got.Type != message.TypeFileInfo {
			t.Fatalf("message type = %q", got.Type)
		}
		var info SenderInfo
		if err := json.Unmarshal(got.Bytes, &info); err != nil {
			t.Fatal(err)
		}
		if info.FilesToTransfer[0].FolderSource != "" || info.FilesToTransfer[0].IsIgnored {
			t.Fatalf("legacy manifest retained sender-only fields: %+v", info.FilesToTransfer[0])
		}
	}

	for _, oversized := range []error{
		fmt.Errorf("encode: %w", message.ErrMessageTooLarge),
		fmt.Errorf("frame: %w", comm.ErrMessageTooLarge),
	} {
		wrapped := wrapLegacyFileInfoSendError(oversized)
		if !strings.Contains(wrapped.Error(), "update croc") || !strings.Contains(wrapped.Error(), "zip/split") {
			t.Fatalf("legacy oversized error = %v", wrapped)
		}
	}
}

func TestNegotiatedReceiverRejectsLegacyFileInfo(t *testing.T) {
	client := manifestTestClient()
	payload, err := json.Marshal(SenderInfo{FilesToTransfer: []FileInfo{{Name: "a", FolderRemote: "."}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.processMessageFileInfo(message.Message{Type: message.TypeFileInfo, Bytes: payload}); err == nil || !strings.Contains(err.Error(), "after negotiating") {
		t.Fatalf("legacy downgrade error = %v", err)
	}
	assertManifestCleared(t, client)
}

func TestChunkedFileInfo200KEntriesEncryptedEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("large manifest test")
	}
	t.Chdir(t.TempDir())
	const (
		fileCount   = 200_000
		folderCount = 37
	)
	modTime := time.Unix(1_700_000_000, 123_000_000).UTC()
	files := make([]FileInfo, fileCount)
	for i := range files {
		files[i] = FileInfo{
			Name:         fmt.Sprintf("file-%06d", i),
			FolderRemote: fmt.Sprintf("folder-%03d", i%100),
			FolderSource: "/private/source",
			Hash:         []byte{byte(i), byte(i >> 8), byte(i >> 16)},
			Size:         int64(i + 1),
			ModTime:      modTime,
			IsCompressed: i%2 == 0,
			IsEncrypted:  i%3 == 0,
			Mode:         0o644,
			TempFile:     i%101 == 0,
			IsIgnored:    true,
			Prepared:     true,
		}
	}
	folders := make([]FileInfo, folderCount)
	for i := range folders {
		folders[i] = FileInfo{
			FolderRemote: fmt.Sprintf("empty-%03d", i),
			FolderSource: "/private/empty-source",
			Mode:         0o755,
			IsIgnored:    true,
		}
	}
	info := SenderInfo{
		FilesToTransfer:        files,
		EmptyFoldersToTransfer: folders,
		TotalNumberFolders:     137,
		MachineID:              "sender-machine",
		ExternalIP:             "203.0.113.10",
		NoCompress:             true,
		HashAlgorithm:          defaultHashAlgorithm,
		ReconnectVersion:       ReconnectVersion,
		NextReconnectRoom:      "large-next-room",
		Features:               []string{perFileCompressionFeature},
	}
	wantHeader := fileInfoManifestHeaderFromSenderInfo(info)
	client := manifestTestClient()
	client.peerInlineMetadata = true
	defer client.closeReceiveFilesystem()
	batchCount := 0
	frameCount := 0
	err := emitChunkedSenderInfo(info, defaultFileInfoManifestLimits, func(outbound message.Message) error {
		frameCount++
		m := encryptedManifestRoundTrip(t, manifestTestKey, outbound)
		switch m.Type {
		case message.TypeFileInfoStart:
			var gotHeader fileInfoManifestHeader
			if err := json.Unmarshal(m.Bytes, &gotHeader); err != nil {
				return err
			}
			if !reflect.DeepEqual(gotHeader, wantHeader) {
				return fmt.Errorf("manifest header mismatch: got %+v want %+v", gotHeader, wantHeader)
			}
			return client.processMessageFileInfoStart(m)
		case message.TypeFileInfoBatch:
			batchCount++
			return client.processMessageFileInfoBatch(m)
		case message.TypeFileInfoEnd:
			_, endErr := client.processMessageFileInfoEnd(m)
			return endErr
		default:
			return fmt.Errorf("unexpected frame type %q", m.Type)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if batchCount < 2 || frameCount != batchCount+2 {
		t.Fatalf("large manifest used %d batches and %d total frames", batchCount, frameCount)
	}
	if len(client.FilesToTransfer) != fileCount || len(client.EmptyFoldersToTransfer) != folderCount {
		t.Fatalf("received %d files and %d folders; want %d and %d", len(client.FilesToTransfer), len(client.EmptyFoldersToTransfer), fileCount, folderCount)
	}
	for i := range files {
		want := fileInfoForWire(files[i])
		if !reflect.DeepEqual(client.FilesToTransfer[i], want) {
			t.Fatalf("file %d metadata mismatch: got %+v want %+v", i, client.FilesToTransfer[i], want)
		}
	}
	for i := range folders {
		want := fileInfoForWire(folders[i])
		if !reflect.DeepEqual(client.EmptyFoldersToTransfer[i], want) {
			t.Fatalf("empty folder %d metadata mismatch: got %+v want %+v", i, client.EmptyFoldersToTransfer[i], want)
		}
	}
	if client.nextReconnectRoom != info.NextReconnectRoom || client.peerReconnectVersion != info.ReconnectVersion || !client.Options.NoCompress {
		t.Fatal("large manifest scalar metadata was not applied")
	}
	if !client.lifecycleSnapshot().FileInfoTransferred {
		t.Fatal("large manifest was not finalized")
	}
}
