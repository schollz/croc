package croc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/schollz/croc/v11/src/bundle"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
)

func bundleTestClient(t *testing.T) *Client {
	t.Helper()
	c := &Client{Options: Options{HashAlgorithm: defaultHashAlgorithm, IsSender: true}, stop: newStop(context.Background()), mutex: new(sync.Mutex), receiveMutex: new(sync.Mutex), FilesHasFinished: make(map[int]struct{})}
	directory := t.TempDir()
	for i, size := range []int{1, 32770, 9000, 60000} {
		name := string(rune('a'+i)) + ".bin"
		if err := os.WriteFile(filepath.Join(directory, name), bytes.Repeat([]byte{byte(i + 1)}, size), 0o640); err != nil {
			t.Fatal(err)
		}
		stat, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		c.FilesToTransfer = append(c.FilesToTransfer, FileInfo{Name: name, FolderRemote: ".", FolderSource: directory, Size: stat.Size(), Mode: stat.Mode(), ModTime: stat.ModTime()})
		c.sourceSnapshots = append(c.sourceSnapshots, nil)
		if err = c.prepareFile(i, defaultHashAlgorithm, new(filePreparationScratch)); err != nil {
			t.Fatal(err)
		}
	}
	c.bundleState().size = 101771
	c.bundleState().request = bundle.Request{ID: 1, Indices: []int{0, 1, 2, 3}, Limits: bundle.NativeLimits()}
	var err error
	c.bundleState().offsets, c.bundleState().size, err = bundle.ValidateRequest(c.bundleState().request, c.bundleMembers(), bundle.NativeLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.removeBundleSpool(); c.closeReceiveFilesystem(); c.stop.Cancel() })
	return c
}

func prepareTestBundle(t *testing.T, c *Client) {
	t.Helper()
	offer, err := c.prepareBundle(c.bundleState().request)
	if err != nil {
		t.Fatal(err)
	}
	c.bundleState().offer = offer
}

func TestBundlePrepareRawPayloadAndPrivateSpool(t *testing.T) {
	c := bundleTestClient(t)
	prepareTestBundle(t, c)
	stat, err := c.bundleState().spool.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if (runtime.GOOS != "windows" && stat.Mode().Perm() != 0o600) || stat.Size() != c.bundleState().size {
		t.Fatal(stat)
	}
	var expected []byte
	for _, file := range c.FilesToTransfer {
		b, err := os.ReadFile(sourceFilePath(file))
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, b...)
	}
	got, err := os.ReadFile(c.bundleState().spool.Name())
	if err != nil || !bytes.Equal(got, expected) {
		t.Fatal("raw concatenation differs", err)
	}
	hash := xxhash.New()
	hash.Write(got)
	if !bytes.Equal(hash.Sum(nil), c.bundleState().offer.Hash) {
		t.Fatal("payload hash differs")
	}
	name := c.bundleState().spool.Name()
	c.removeBundleSpool()
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("spool leaked")
	}
}

func TestBundleSourceChangeAborts(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot", true: "same-size-content"}[during], func(t *testing.T) {
			c := bundleTestClient(t)
			file := c.FilesToTransfer[1]
			if err := os.WriteFile(sourceFilePath(file), bytes.Repeat([]byte("x"), int(file.Size)), file.Mode); err != nil {
				t.Fatal(err)
			}
			if during {
				if err := os.Chtimes(sourceFilePath(file), file.ModTime, file.ModTime); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.prepareBundle(c.bundleState().request); err == nil {
				t.Fatal("source change accepted")
			}
		})
	}
}

func TestBundleVerifyBeforePublicationAndCleanup(t *testing.T) {
	for _, corruption := range []string{"payload", "member", "none"} {
		t.Run(corruption, func(t *testing.T) {
			c := bundleTestClient(t)
			prepareTestBundle(t, c)
			t.Chdir(t.TempDir())
			c.Options.IsSender = false
			if corruption == "payload" {
				if _, err := c.bundleState().spool.WriteAt([]byte("X"), 0); err != nil {
					t.Fatal(err)
				}
			}
			if corruption == "member" {
				c.FilesToTransfer[2].Hash = make([]byte, 8)
			}
			err := c.verifyAndCommitBundle(c.bundleState().spool)
			if corruption != "none" {
				if err == nil {
					t.Fatal("corrupt bundle accepted")
				}
				entries, _ := os.ReadDir(".")
				if len(entries) != 0 {
					t.Fatalf("corruption published or leaked staging: %v", entries)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.finishedNum != len(c.FilesToTransfer) {
				t.Fatalf("completion count = %d", c.finishedNum)
			}
			for i, file := range c.FilesToTransfer {
				got, err := os.ReadFile(file.Name)
				if err != nil {
					t.Fatal(err)
				}
				want, _ := os.ReadFile(sourceFilePath(file))
				if !bytes.Equal(got, want) {
					t.Fatal("member differs")
				}
				if _, ok := c.FilesHasFinished[i]; !ok {
					t.Fatal("missing logical completion")
				}
				stat, _ := os.Stat(file.Name)
				if stat.Size() != file.Size || stat.ModTime().Sub(file.ModTime).Abs() > time.Millisecond {
					t.Fatal("metadata differs")
				}
			}
			entries, _ := os.ReadDir(".")
			if len(entries) != 4 {
				t.Fatal("staging leaked", entries)
			}
		})
	}
}

func TestBundleCommitConflictRetainsCompletedPrefix(t *testing.T) {
	c := bundleTestClient(t)
	prepareTestBundle(t, c)
	t.Chdir(t.TempDir())
	c.Options.IsSender = false
	if err := os.WriteFile(c.FilesToTransfer[2].Name, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.verifyAndCommitBundle(c.bundleState().spool); err == nil {
		t.Fatal("late destination collision accepted")
	}
	for i := range 2 {
		if _, err := os.Stat(c.FilesToTransfer[i].Name); err != nil {
			t.Fatal("committed prefix lost", err)
		}
	}
	got, _ := os.ReadFile(c.FilesToTransfer[2].Name)
	if string(got) != "unrelated" {
		t.Fatal("overwrote late collision")
	}
	if _, err := os.Stat(c.FilesToTransfer[3].Name); !os.IsNotExist(err) {
		t.Fatal("published suffix")
	}
	entries, _ := os.ReadDir(".")
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".croc-") {
			t.Fatal("staging leaked")
		}
	}
}

func TestBundleCancellationAndPreflightCache(t *testing.T) {
	c := bundleTestClient(t)
	prepareTestBundle(t, c)
	t.Chdir(t.TempDir())
	c.Options.IsSender = false
	file := c.FilesToTransfer[0]
	if err := os.WriteFile(file.Name, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := receiveOverwriteInput
	t.Cleanup(func() { receiveOverwriteInput = input })
	count := 0
	receiveOverwriteInput = func(context.Context, string) (string, error) { count++; return "y", nil }
	for range 2 {
		decision, err := c.preflightBundleFile(0)
		if err != nil || !decision.requested || decision.eligible {
			t.Fatal(decision, err)
		}
	}
	if count != 1 {
		t.Fatal("prompt repeated", count)
	}
	c.stop.Cancel()
	if err := c.verifyAndCommitBundle(c.bundleState().spool); err == nil {
		t.Fatal("ignored cancellation")
	}
	entries, _ := os.ReadDir(".")
	if len(entries) != 1 {
		t.Fatal("cancelled staging leaked")
	}
}

func TestBundleNegotiationAndLifecycleRejectLegacyMessages(t *testing.T) {
	c := bundleTestClient(t)
	if err := c.acceptBundleManifest(SenderInfo{Features: []string{bundle.Feature}}); err == nil {
		t.Fatal("missing limits accepted")
	}
	limits := bundle.NativeLimits()
	if err := c.acceptBundleManifest(SenderInfo{Features: []string{bundle.Feature}, BundleLimits: &limits}); err != nil {
		t.Fatal(err)
	}
	if c.bundleSessionEligible() {
		t.Fatal("inferred handshake support")
	}
	c.bundles.peerHandshake = true
	if !c.bundleSessionEligible() {
		t.Fatal("negotiated support not accepted")
	}
	request, _ := json.Marshal(c.bundleState().request)
	if err := c.processBundleMessage(message.Message{Type: message.TypeBundleRequest, Bytes: request}, new(transferAttemptState)); err == nil {
		t.Fatal("request before metadata accepted")
	}
	if err := c.bundleState().lifecycle.Request(1); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []message.Type{message.TypeFinished, message.TypeRecipientReady, message.TypeCloseSender, message.TypeCloseRecipient} {
		payload, err := message.Encode(c.Key, message.Message{Type: kind})
		if err != nil {
			t.Fatal(err)
		}
		c.Key = make([]byte, 32)
		payload, err = message.Encode(c.Key, message.Message{Type: kind})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.processMessage(payload, new(transferAttemptState)); err == nil {
			t.Fatal("accepted legacy completion during bundle", kind)
		}
	}
}

func TestBundleReconnectUsesLogicalIdentity(t *testing.T) {
	c := bundleTestClient(t)
	original := c.FilesToTransfer[0]
	accepted := original
	accepted.Name = "a (1).bin"
	c.bundleCommitted = map[string]FileInfo{bundleFileIdentity(original): accepted}
	c.FilesHasFinished[0] = struct{}{}
	// A preceding manifest insertion changes every index; only the validated
	// identity may recover the accepted rename, and it still needs disk preflight.
	inserted := c.FilesToTransfer[3]
	inserted.Name = "inserted.bin"
	c.FilesToTransfer = append([]FileInfo{inserted}, c.FilesToTransfer...)
	if err := c.restoreBundleDestinations(); err != nil {
		t.Fatal(err)
	}
	if c.FilesToTransfer[1].Name != accepted.Name || len(c.FilesHasFinished) != 0 {
		t.Fatal("trusted stale index or lost accepted name")
	}
	changed := original
	changed.Hash = make([]byte, 8)
	c.FilesToTransfer = []FileInfo{changed}
	if err := c.restoreBundleDestinations(); err != nil {
		t.Fatal(err)
	}
	if c.FilesToTransfer[0].Name != original.Name {
		t.Fatal("retained completion matched changed contents")
	}
}

func TestBundleManifestRoundTripCarriesLimits(t *testing.T) {
	limits := bundle.NativeLimits()
	info := SenderInfo{Features: []string{bundle.Feature}, BundleLimits: &limits}
	got := fileInfoManifestHeaderFromSenderInfo(info).senderInfo(nil, nil)
	if got.BundleLimits == nil || *got.BundleLimits != limits {
		t.Fatal("lost chunked manifest limits")
	}
}

func TestBundleReceiveUniqueExactOutOfOrderChunks(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "spool")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	chunks, _ := bundle.NewChunks(bundle.ChunkSize*2 + 1)
	unit := &bundleReceiveUnit{id: 1, file: file, chunks: chunks}
	attempt := &transferAttemptState{bundleReceived: make(chan *bundleReceiveUnit, 1)}
	frame := func(offset uint64, length int) []byte {
		data := make([]byte, 8+length)
		binary.LittleEndian.PutUint64(data, offset)
		return data
	}
	for _, bad := range [][]byte{frame(1, 1), frame(0, 1), frame(uint64(bundle.ChunkSize), 1)} {
		if unit.accept(bad, attempt) == nil {
			t.Fatal("accepted short/unaligned chunk")
		}
	}
	if err = unit.accept(frame(uint64(bundle.ChunkSize), int(bundle.ChunkSize)), attempt); err != nil {
		t.Fatal(err)
	}
	if err = unit.accept(frame(uint64(bundle.ChunkSize), int(bundle.ChunkSize)), attempt); err == nil {
		t.Fatal("accepted duplicate")
	}
	if err = unit.accept(frame(uint64(bundle.ChunkSize*2), 1), attempt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-attempt.bundleReceived:
		t.Fatal("completed with missing chunk")
	default:
	}
	if err = unit.accept(frame(0, int(bundle.ChunkSize)), attempt); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-attempt.bundleReceived:
		if got != unit {
			t.Fatal("wrong unit")
		}
	default:
		t.Fatal("missing completion")
	}
	if err = unit.accept(frame(0, int(bundle.ChunkSize)), attempt); err == nil {
		t.Fatal("accepted late data")
	}
}

func TestBundleJoinDetachesLateWorkersBeforeNextUnit(t *testing.T) {
	c := bundleTestClient(t)
	queue := newRequestedChunkQueue(nil, 1, 32768, nil)
	queue.workers.Add(1)
	attempt := new(transferAttemptState)
	c.senderChunkQueue, c.senderDataFile, c.senderDataAttempt = queue, new(os.File), attempt
	joined := make(chan struct{})
	go func() { c.joinSenderUnit(); close(joined) }()
	deadline := time.Now().Add(time.Second)
	for {
		c.senderDataMu.Lock()
		detached := c.senderChunkQueue == nil
		c.senderDataMu.Unlock()
		if detached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unit not detached")
		}
		time.Sleep(time.Millisecond)
	}
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	c.startLateSenderWorker(comm.New(local), attempt)
	select {
	case <-joined:
		t.Fatal("closed before worker joined")
	default:
	}
	queue.workers.Done()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("workers did not join")
	}
}

func TestBundleReceiveWriteFailureCleansPrivatePayload(t *testing.T) {
	c := bundleTestClient(t)
	spool, err := os.CreateTemp(t.TempDir(), "croc-bundle-receive-*")
	if err != nil {
		t.Fatal(err)
	}
	name := spool.Name()
	if err = spool.Close(); err != nil {
		t.Fatal(err)
	}
	chunks, err := bundle.NewChunks(1)
	if err != nil {
		t.Fatal(err)
	}
	unit := &bundleReceiveUnit{id: 1, file: spool, chunks: chunks}
	c.bundleState().spool = spool
	c.bundleState().receive.Store(unit)
	attempt := &transferAttemptState{bundleReceived: make(chan *bundleReceiveUnit, 1)}
	if err = unit.accept(make([]byte, 9), attempt); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("write failure was lost: %v", err)
	}
	select {
	case <-attempt.bundleReceived:
		t.Fatal("failed write completed the payload")
	default:
	}
	c.finishTransferAttempt(attempt)
	if _, err = os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("failed payload leaked: %v", err)
	}
}

func TestBundleControlDisconnectRemainsReconnectable(t *testing.T) {
	c := bundleTestClient(t)
	local, remote := net.Pipe()
	remote.Close()
	defer local.Close()
	c.conn = []*comm.Comm{comm.New(local)}
	c.peerReconnectVersion, c.reconnectVersion = ReconnectVersion, ReconnectVersion
	c.nextReconnectRoom = "authenticated-next-attempt"
	err := c.sendBundle(message.TypeBundleReady, bundle.Ack{ID: 1})
	if !isTransferDisconnectError(err) || !c.canRetryTransfer(err, 0) {
		t.Fatalf("control disconnect lost reconnect eligibility: %v", err)
	}
	if c.canRetryTransfer(fmt.Errorf("bundle member hash mismatch"), 0) {
		t.Fatal("verification error became reconnectable")
	}
}

func TestBundlePlanRechecksCompletedIndividualFile(t *testing.T) {
	c := bundleTestClient(t)
	t.Chdir(t.TempDir())
	c.Options.IsSender, c.Options.Overwrite = false, true
	c.FilesToTransfer = c.FilesToTransfer[:1]
	c.FilesToTransfer[0].Size = bundle.MaxFileBytes + 1
	if err := os.WriteFile(c.FilesToTransfer[0].Name, make([]byte, bundle.MaxFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	c.bundles.peerHandshake, c.bundles.manifest = true, true
	c.bundles.peerLimits = bundle.NativeLimits()
	c.receiveChoices = map[int]bool{0: true}
	c.updateLifecycle(func(state *transferLifecycle) {
		state.ChannelSecured, state.FileInfoTransferred, state.RecipientRequested = true, true, true
	})
	c.Key = make([]byte, 32)
	verificationFailure := errors.New("destination read failed during final verification")
	previousHash := receiveFileHash
	t.Cleanup(func() { receiveFileHash = previousHash })
	checked := 0
	receiveFileHash = func(context.Context, string, string, ...bool) ([]byte, error) {
		checked++
		return nil, verificationFailure
	}
	payload, err := message.Encode(c.Key, message.Message{Type: message.TypeCloseRecipient})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.processMessage(payload, new(transferAttemptState)); !errors.Is(err, verificationFailure) {
		t.Fatalf("final individual verification was bypassed: %v", err)
	}
	if checked != 1 {
		t.Fatalf("checked destination %d times", checked)
	}
	if _, done := c.FilesHasFinished[0]; done {
		t.Fatal("failed verification marked the individual file complete")
	}
}

func TestBundleReconnectRestartsFirstUnfinishedPayload(t *testing.T) {
	for _, connectionIndex := range []int{0, 1} {
		t.Run(fmt.Sprintf("connection-%d", connectionIndex), func(t *testing.T) {
			sourceDir := t.TempDir()
			destination := t.TempDir()
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			t.Setenv("TMP", temporary)
			t.Setenv("TEMP", temporary)
			t.Chdir(destination)
			var paths []string
			empty := filepath.Join(sourceDir, "-empty.bin")
			if err := os.WriteFile(empty, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, empty)
			for i := range 4 {
				name := filepath.Join(sourceDir, fmt.Sprintf("%d.bin", i))
				if err := os.WriteFile(name, bytes.Repeat([]byte{byte(i + 1)}, 256<<10), 0o600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, name)
			}
			port, stopRelay := startReconnectRelay(t)
			defer stopRelay()
			options := Options{SharedSecret: fmt.Sprintf("bundle-reconnect-%d", time.Now().UnixNano()), RelayAddress: "127.0.0.1:" + port, RelayPassword: "pass123", NoPrompt: true, DisableLocal: true, DisableClipboard: true, Curve: "siec", Overwrite: true, NoCompress: true, NoMultiplexing: true}
			sendOptions := options
			sendOptions.IsSender = true
			sendOptions.Transport = TransportRelay
			sendOptions.ThrottleUpload = "512K"
			sender, err := newTestClient(sendOptions)
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := newTestClient(options)
			if err != nil {
				t.Fatal(err)
			}
			files, folders, count, err := GetFilesInfo(paths, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			errors := make(chan error, 2)
			go func() { errors <- sender.Send(files, folders, count) }()
			go func() {
				if err := waitHashed(sender); err != nil {
					errors <- err
					return
				}
				errors <- receiver.Receive()
			}()
			dropped := make(chan bool, 1)
			go func() {
				active := waitForReconnectCondition(5*time.Second, func() bool {
					sender.senderDataMu.Lock()
					queue := sender.senderChunkQueue
					sender.senderDataMu.Unlock()
					if queue == nil || queue.progress == nil {
						return false
					}
					queue.mu.Lock()
					progressing := queue.completed > 0 && queue.completed < queue.total
					queue.mu.Unlock()
					return progressing && sender.connection(connectionIndex) != nil
				})
				if active {
					sender.connection(connectionIndex).Close()
				}
				dropped <- active
			}()
			waitForTransferResults(t, errors, 2, 20*time.Second, sender, receiver)
			if !<-dropped {
				t.Fatal("never interrupted a live first bundle")
			}
			if sender.Options.RoomName == sender.baseRoomName {
				t.Fatal("bundle session did not reconnect")
			}
			for _, source := range paths {
				want, _ := os.ReadFile(source)
				got, err := os.ReadFile(filepath.Base(source))
				if err != nil || sha256.Sum256(got) != sha256.Sum256(want) {
					t.Fatal("reconnected member mismatch", err)
				}
			}
			for _, directory := range []string{temporary, destination} {
				entries, _ := os.ReadDir(directory)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "croc-bundle-") || strings.HasPrefix(entry.Name(), ".croc-bundle-") {
						t.Fatal("bundle storage leaked after reconnect", entry.Name())
					}
				}
			}
			if receiver.numberOfTransferredFiles != len(paths) || receiver.finishedNum != len(paths) {
				t.Fatalf("logical completions after restart = %d/%d", receiver.numberOfTransferredFiles, receiver.finishedNum)
			}
		})
	}
}

func TestBundleJoinedCleanupErrorsHideEveryPrivatePath(t *testing.T) {
	cause := errors.New("storage failure")
	original := errors.Join(
		transferDisconnectError{err: errors.New("connection closed")},
		bundleCleanupError{errors.Join(
			&os.PathError{Op: "remove", Path: "/tmp/croc-bundle-send-private", Err: cause},
			&os.LinkError{Op: "link", Old: ".croc-bundle-private/member", New: "accepted.txt", Err: cause},
		)},
	)
	visible := bundleVisibleError(original)
	if strings.Contains(visible.Error(), "croc-bundle-") || !errors.Is(visible, cause) || isTransferDisconnectError(visible) {
		t.Fatal(visible)
	}
}
