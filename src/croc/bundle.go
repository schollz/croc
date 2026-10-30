package croc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/denisbrodbeck/machineid"
	"github.com/schollz/croc/v11/src/bundle"
	"github.com/schollz/croc/v11/src/comm"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/receivefs"
)

// Keep private spool/staging names out of ordinary error output while retaining
// the underlying error identity for reconnect and filesystem checks.
type bundleStorageError struct {
	err     error
	display string
}

func (e bundleStorageError) Error() string {
	return e.display
}
func (e bundleStorageError) Unwrap() error { return e.err }
func bundleVisibleError(err error) error {
	if err == nil {
		return nil
	}
	display := err.Error()
	// Cleanup may join staging and spool failures. Redact every leaf while
	// preserving the original error tree for identity and reconnect decisions.
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			if child != nil {
				display = strings.ReplaceAll(display, child.Error(), bundleVisibleError(child).Error())
			}
		}
	case interface{ Unwrap() error }:
		if child := wrapped.Unwrap(); child != nil {
			display = strings.ReplaceAll(display, child.Error(), bundleVisibleError(child).Error())
		}
	}
	switch specific := err.(type) {
	case *os.PathError:
		if strings.Contains(specific.Path, "croc-bundle-") {
			display = "temporary transfer storage: " + specific.Op + ": " + specific.Err.Error()
		}
	case *os.LinkError:
		if strings.Contains(specific.Old, "croc-bundle-") || strings.Contains(specific.New, "croc-bundle-") {
			display = "temporary transfer storage: " + specific.Op + ": " + specific.Err.Error()
		}
	}
	if display != err.Error() {
		return bundleStorageError{err, display}
	}
	return err
}

type bundlePreflight struct{ requested, eligible bool }

// Negotiated capabilities and retained logical identities outlive a connection.
type bundleClientState struct {
	identities    []string
	peerHandshake bool
	manifest      bool
	peerLimits    bundle.Limits
	active        *bundleAttemptState
}

// The control loop owns lifecycle and private storage. Workers see only receive.
type bundleAttemptState struct {
	attempt             *transferAttemptState
	receive             atomic.Pointer[bundleReceiveUnit]
	lastClosed, readyAt time.Time
	lifecycle           bundle.Lifecycle
	request             bundle.Request
	offsets             []int64
	size                int64
	offer               bundle.Offer
	spool               *os.File
	stageRoot           *receivefs.Root
	stageDir            string
	publication         map[string]bool
}

func (c *Client) bundleState() *bundleAttemptState {
	if c.bundles.active == nil {
		c.bundles.active = new(bundleAttemptState)
	}
	return c.bundles.active
}

// Cleanup failures are terminal even when joined with a transport disconnect.
// Keep ownership until removal succeeds, allowing teardown to retry disposal.
type bundleCleanupError struct{ err error }

func (e bundleCleanupError) Error() string {
	return "discard private transfer storage: " + e.err.Error()
}
func (e bundleCleanupError) Unwrap() error { return e.err }

// Only this immutable unit is shared with receive workers. Lifecycle, file
// completions, verification and publication stay with the control owner.
type bundleReceiveUnit struct {
	id         uint32
	file       *os.File
	compressed bool
	mu         sync.Mutex
	chunks     *bundle.Chunks
	closed     bool
	progress   func(int64, int)
}

func (u *bundleReceiveUnit) accept(data []byte, attempt *transferAttemptState) error {
	if len(data) < 9 || len(data) > maxDecompressedChunkSize {
		return errors.New("invalid bundle data chunk")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return errors.New("bundle data after completed payload")
	}
	position := binary.LittleEndian.Uint64(data[:8])
	done, err := u.chunks.Accept(position, len(data)-8)
	if err != nil {
		return err
	}
	if _, err = u.file.WriteAt(data[8:], int64(position)); err != nil {
		return err
	}
	if u.progress != nil {
		u.progress(int64(position), len(data)-8)
	}
	if done {
		u.closed = true
		attempt.bundleReceived <- u
	}
	return nil
}

func (c *Client) startReceiveData(index int, connection *comm.Comm, attempt *transferAttemptState) {
	attempt.dataMu.Lock()
	if attempt.dataClosed {
		attempt.dataMu.Unlock()
		connection.Close()
		return
	}
	attempt.dataWorkers.Add(1)
	attempt.dataMu.Unlock()
	go func() { defer attempt.dataWorkers.Done(); c.receiveData(index, connection, attempt) }()
}

func (c *Client) finishTransferAttempt(attempt *transferAttemptState) error {
	if attempt.cancel != nil {
		attempt.cancel(context.Canceled)
	}
	attempt.cancelTailcatSetup()
	attempt.closeTailcatPending()
	// Prevent new workers before waiting, including delayed data connections.
	attempt.dataMu.Lock()
	attempt.dataClosed = true
	attempt.dataMu.Unlock()
	c.closeAttempt()
	c.joinSenderUnit()
	c.fread = nil
	attempt.dataWorkers.Wait()
	c.bundleState().receive.Store(nil)
	return errors.Join(c.removeBundleStage(), c.removeBundleSpool())
}

func (a *transferAttemptState) contextDone() <-chan struct{} {
	if a.ctx == nil {
		return nil
	}
	return a.ctx.Done()
}

func (c *Client) bundleContextErr() error {
	if err := c.clientContext().Err(); err != nil {
		return err
	}
	if attempt := c.bundleState().attempt; attempt != nil && attempt.ctx != nil {
		return context.Cause(attempt.ctx)
	}
	return nil
}

func (c *Client) removeBundleSpool() error {
	state := c.bundleState()
	if spool := state.spool; spool != nil {
		closeErr := spool.Close()
		if errors.Is(closeErr, os.ErrClosed) {
			closeErr = nil
		}
		removeErr := os.Remove(spool.Name())
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		if removeErr == nil {
			state.spool = nil
		}
		if err := errors.Join(closeErr, removeErr); err != nil {
			return bundleCleanupError{err}
		}
	}
	return nil
}

func (c *Client) removeBundleStage() error {
	state := c.bundleState()
	if state.stageDir == "" {
		return nil
	}
	if err := state.stageRoot.RemoveAll(state.stageDir); err != nil {
		return bundleCleanupError{err}
	}
	state.stageDir, state.stageRoot = "", nil
	return nil
}

func (c *Client) senderManifestFeatures() []string {
	return []string{perFileCompressionFeature, bundle.Feature}
}
func (c *Client) senderBundleLimits() *bundle.Limits { limits := bundle.NativeLimits(); return &limits }

func (c *Client) acceptBundleManifest(info SenderInfo) error {
	advertised := supportsFeature(info.Features, bundle.Feature)
	if info.BundleLimits != nil {
		if err := info.BundleLimits.Validate(); err != nil {
			return err
		}
	}
	if advertised && info.BundleLimits == nil {
		return errors.New("bundle capability without limits")
	}
	c.bundles.manifest = advertised && !info.Ask
	if info.BundleLimits != nil {
		c.bundles.peerLimits = *info.BundleLimits
	}
	return nil
}

func (c *Client) bundleTransferEligible() bool {
	return !c.Options.Stdout && !c.Options.SendingText && !c.Options.Ask && !c.Options.ZipFolder && c.Options.HashAlgorithm == defaultHashAlgorithm
}

func (c *Client) bundleSessionEligible() bool {
	return c.bundles.peerHandshake && c.bundles.manifest && c.bundleTransferEligible()
}

func bundleFileEligible(file FileInfo) bool {
	return file.Mode.IsRegular() && file.Symlink == "" && !file.TempFile && file.Prepared && len(file.Hash) == 8 && file.Size > 0 && file.Size <= bundle.MaxFileBytes
}

func (c *Client) bundleMembers() []bundle.Member {
	members := make([]bundle.Member, len(c.FilesToTransfer))
	for i, file := range c.FilesToTransfer {
		members[i] = bundle.Member{Size: file.Size, Eligible: bundleFileEligible(file), Requested: true}
	}
	return members
}

func (c *Client) preflightBundleFile(index int) (bundlePreflight, error) {
	decision, err := c.preflightReceiveFile(index)
	if err != nil {
		return bundlePreflight{}, err
	}
	eligible := (!decision.requested || decision.fresh) && bundleFileEligible(c.FilesToTransfer[index])
	if decision.requested && eligible {
		state := c.bundleState()
		folder := path.Dir(path.Join(c.FilesToTransfer[index].FolderRemote, c.FilesToTransfer[index].Name))
		supported, known := state.publication[folder]
		if !known {
			root, err := c.receiveFilesystem()
			if err != nil {
				return bundlePreflight{}, err
			}
			supported, err = root.CanPublishNew(folder)
			if err != nil {
				return bundlePreflight{}, err
			}
			if state.publication == nil {
				state.publication = make(map[string]bool)
			}
			state.publication[folder] = supported
		}
		eligible = supported
	}
	return bundlePreflight{requested: decision.requested, eligible: eligible}, nil
}

func (c *Client) requestNextBundleUnit() error {
	for {
		limits, err := bundle.Effective(bundle.NativeLimits(), c.bundles.peerLimits)
		if err != nil {
			return err
		}
		unit, err := bundle.Next(len(c.FilesToTransfer), 0, limits, func(index int) (bundle.Member, error) {
			decision, err := c.preflightBundleFile(index)
			return bundle.Member{Size: c.FilesToTransfer[index].Size, Requested: decision.requested, Eligible: decision.eligible}, err
		})
		if err != nil {
			return err
		}
		indices := unit.Indices
		if len(indices) == 0 {
			return c.recipientGetFileReady(true)
		}
		if len(indices) < bundle.MinFiles {
			index := indices[0]
			file := c.FilesToTransfer[index]
			if file.Size == 0 || file.Symlink != "" {
				if err = c.createEmptyFileAndFinish(file, index); err != nil {
					return err
				}
				c.FilesHasFinished[index] = struct{}{}
				c.numberOfTransferredFiles++
				continue
			}
			c.FilesToTransferCurrentNum = index
			c.numberOfTransferredFiles++
			return c.recipientGetFileReady(false)
		}
		if c.bundleState().lifecycle.LastID == ^uint32(0) {
			return errors.New("bundle IDs exhausted")
		}
		id := c.bundleState().lifecycle.LastID + 1
		machine, _ := machineid.ID()
		request := bundle.Request{ID: id, Indices: indices, Limits: bundle.NativeLimits(), MachineID: machine, ReconnectVersion: c.reconnectVersion}
		if c.peerInlineMetadata {
			request.ExternalIP = c.advertisedExternalIP()
		}
		offsets, size := unit.Offsets, unit.Size
		if err = c.bundleState().lifecycle.Request(id); err != nil {
			return err
		}
		c.bundleState().request, c.bundleState().offsets, c.bundleState().size = request, offsets, size
		c.markTransferStarted()
		gap := time.Duration(0)
		if !c.bundleState().lastClosed.IsZero() {
			gap = time.Since(c.bundleState().lastClosed)
		}
		log.Debugf("bundle timing id=%d phase=request members=%d bytes=%d gap=%s", id, len(indices), size, gap)
		return c.sendBundle(message.TypeBundleRequest, request)
	}
}

func (c *Client) sendBundle(kind message.Type, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > bundle.MaxControlBytes {
		return errors.New("bundle control exceeds 16 KiB")
	}
	frame, err := message.Encode(c.Key, message.Message{Type: kind, Bytes: data})
	if err != nil {
		return err
	}
	if err = c.connection(0).Send(frame); err != nil {
		if ctxErr := c.bundleContextErr(); ctxErr != nil {
			return ctxErr
		}
		return transferDisconnectError{err: err}
	}
	return nil
}

func (c *Client) prepareBundle(request bundle.Request) (bundle.Offer, error) {
	started := time.Now()
	if c.bundleState().spool != nil {
		return bundle.Offer{}, errors.New("previous bundle payload still owned")
	}
	spool, err := os.CreateTemp("", "croc-bundle-send-*")
	if err != nil {
		return bundle.Offer{}, err
	}
	c.bundleState().spool = spool
	payloadHash := xxhash.New()
	buffer := make([]byte, 32<<10)
	for _, index := range request.Indices {
		if err = c.bundleContextErr(); err != nil {
			return bundle.Offer{}, err
		}
		if err = c.validateSourceUnchanged(index); err != nil {
			return bundle.Offer{}, err
		}
		info := c.FilesToTransfer[index]
		file, openErr := os.Open(sourceFilePath(info))
		if openErr != nil {
			return bundle.Offer{}, openErr
		}
		before, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return bundle.Offer{}, statErr
		}
		if !sourceInfoMatches(info, c.sourceSnapshots[index], before) {
			file.Close()
			return bundle.Offer{}, errors.New("bundle source changed before copy")
		}
		memberHash := xxhash.New()
		reader := &bundleContextReader{client: c, reader: io.LimitReader(file, info.Size)}
		copied, copyErr := io.CopyBuffer(io.MultiWriter(spool, payloadHash, memberHash), reader, buffer)
		after, statErr := file.Stat()
		closeErr := file.Close()
		if copyErr != nil {
			return bundle.Offer{}, copyErr
		}
		if statErr != nil {
			return bundle.Offer{}, statErr
		}
		if closeErr != nil {
			return bundle.Offer{}, closeErr
		}
		if copied != info.Size || !sourceInfoMatches(info, before, after) || !bytes.Equal(memberHash.Sum(nil), info.Hash) {
			return bundle.Offer{}, errors.New("bundle source changed during copy")
		}
		if err = c.validateSourceUnchanged(index); err != nil {
			return bundle.Offer{}, err
		}
	}
	compressed := false
	if !c.Options.NoCompress {
		compressed, _ = shouldCompressFile(spool.Name(), make([]byte, compressionSampleSize), nil)
	}
	log.Debugf("bundle timing id=%d phase=prepare duration=%s bytes=%d members=%d", request.ID, time.Since(started), c.bundleState().size, len(request.Indices))
	return bundle.Offer{ID: request.ID, Size: c.bundleState().size, Hash: payloadHash.Sum(nil), Compressed: compressed}, nil
}

type bundleContextReader struct {
	client *Client
	reader io.Reader
}

func (r *bundleContextReader) Read(p []byte) (int, error) {
	if err := r.client.bundleContextErr(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (c *Client) processBundleMessage(m message.Message, attempt *transferAttemptState) error {
	if !c.bundles.peerHandshake || !c.bundleTransferEligible() || !c.lifecycleSnapshot().FileInfoTransferred {
		return errors.New("bundle message without negotiated support")
	}
	s := c.bundleState()
	switch m.Type {
	case message.TypeBundleRequest:
		if !c.Options.IsSender || c.lifecycleSnapshot().RecipientRequested {
			return errors.New("unexpected bundle request")
		}
		var request bundle.Request
		if err := bundle.Decode(m.Bytes, &request); err != nil {
			return err
		}
		offsets, size, err := bundle.ValidateRequest(request, c.bundleMembers(), bundle.NativeLimits())
		if err != nil {
			return err
		}
		if err = s.lifecycle.Request(request.ID); err != nil {
			return err
		}
		s.request, s.offsets, s.size = request, offsets, size
		c.peerReconnectVersion = request.ReconnectVersion
		if c.peerInlineMetadata {
			c.ExternalIPConnected = preferredPeerIP(c.ExternalIPConnected, request.ExternalIP)
		}
		c.markTransferStarted()
		offer, err := c.prepareBundle(request)
		if err != nil {
			return err
		}
		if err = s.lifecycle.Advance(request.ID, "offer"); err != nil {
			return err
		}
		s.offer = offer
		return c.sendBundle(message.TypeBundleOffer, offer)
	case message.TypeBundleOffer:
		if c.Options.IsSender || !c.bundles.manifest {
			return errors.New("unexpected bundle offer")
		}
		var offer bundle.Offer
		if err := bundle.Decode(m.Bytes, &offer); err != nil {
			return err
		}
		if err := bundle.ValidateOffer(offer, s.request.ID, s.size, c.Options.NoCompress); err != nil {
			return err
		}
		if err := s.lifecycle.Advance(offer.ID, "offer"); err != nil {
			return err
		}
		if s.spool != nil {
			return errors.New("previous bundle payload still owned")
		}
		spool, err := os.CreateTemp("", "croc-bundle-receive-*")
		if err != nil {
			return err
		}
		s.spool = spool
		if err = spool.Truncate(offer.Size); err != nil {
			return err
		}
		chunks, err := bundle.NewChunks(offer.Size)
		if err != nil {
			return err
		}
		u := &bundleReceiveUnit{id: offer.ID, file: spool, compressed: offer.Compressed, chunks: chunks, progress: c.newBundleProgress(s.request.Indices, s.offsets)}
		s.offer = offer
		c.bundleState().receive.Store(u)
		if err = s.lifecycle.Advance(offer.ID, "ready"); err != nil {
			return err
		}
		s.readyAt = time.Now()
		return c.sendBundle(message.TypeBundleReady, bundle.Ack{ID: offer.ID})
	case message.TypeBundleReady, message.TypeBundleDone, message.TypeBundleClosed:
		var ack bundle.Ack
		if err := bundle.Decode(m.Bytes, &ack); err != nil {
			return err
		}
		if m.Type == message.TypeBundleReady {
			if !c.Options.IsSender {
				return errors.New("unexpected bundle ready")
			}
			if err := s.lifecycle.Advance(ack.ID, "ready"); err != nil {
				return err
			}
			c.startSendProgress()
			c.startSenderUnit(attempt, s.spool, s.size, s.offer.Compressed, nil, c.newBundleProgress(s.request.Indices, s.offsets))
			connections := 0
			for i, connection := range c.connectionsSnapshot() {
				if i > 0 && connection != nil {
					connections++
				}
			}
			log.Debugf("bundle timing id=%d phase=data-start connections=%d", ack.ID, connections)
			return nil
		}
		if m.Type == message.TypeBundleDone {
			if !c.Options.IsSender {
				return errors.New("unexpected bundle done")
			}
			if err := s.lifecycle.Advance(ack.ID, "done"); err != nil {
				return err
			}
			c.joinSenderUnit()
			for range s.request.Indices {
				c.fmtPrintUpdate()
			}
			if err := c.removeBundleSpool(); err != nil {
				return err
			}
			if err := s.lifecycle.Advance(ack.ID, "closed"); err != nil {
				return err
			}
			s.lastClosed = time.Now()
			return c.sendBundle(message.TypeBundleClosed, ack)
		}
		if c.Options.IsSender {
			return errors.New("unexpected bundle closed")
		}
		c.bundleState().receive.Store(nil)
		if err := c.removeBundleSpool(); err != nil {
			return err
		}
		if err := s.lifecycle.Advance(ack.ID, "closed"); err != nil {
			return err
		}
		s.lastClosed = time.Now()
		return nil
	}
	return errors.New("unknown bundle operation")
}

func (c *Client) finishReceivedBundle(unit *bundleReceiveUnit) error {
	s := c.bundleState()
	if c.bundleState().receive.Load() != unit || s.lifecycle.ActiveID != unit.id || s.lifecycle.Phase != "ready" {
		return errors.New("unexpected bundle completion")
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	if !unit.closed || unit.chunks.Bytes != s.size {
		return errors.New("incomplete bundle payload")
	}
	log.Debugf("bundle timing id=%d phase=data-complete duration=%s bytes=%d", unit.id, time.Since(s.readyAt), s.size)
	if err := c.verifyAndCommitBundle(unit.file); err != nil {
		return err
	}
	if err := s.lifecycle.Advance(unit.id, "done"); err != nil {
		return err
	}
	return c.sendBundle(message.TypeBundleDone, bundle.Ack{ID: unit.id})
}

// All verification precedes any publication. Staged files are private, and
// every cleanup path leaves already-published members in place.
func (c *Client) verifyAndCommitBundle(spool *os.File) (result error) {
	started := time.Now()
	s := c.bundleState()
	payloadHash := xxhash.New()
	if _, err := io.Copy(payloadHash, &bundleContextReader{c, io.NewSectionReader(spool, 0, s.size)}); err != nil {
		return err
	}
	if !bytes.Equal(payloadHash.Sum(nil), s.offer.Hash) {
		return errors.New("bundle payload hash mismatch")
	}
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	if s.stageDir != "" {
		return errors.New("previous bundle staging still owned")
	}
	stageDir, err := root.CreateTempDir(".", ".croc-bundle-")
	if err != nil {
		return err
	}
	s.stageRoot, s.stageDir = root, stageDir
	defer func() { result = errors.Join(result, c.removeBundleStage()) }()
	staged := make([]string, len(s.request.Indices))
	for i, index := range s.request.Indices {
		if err = c.bundleContextErr(); err != nil {
			return err
		}
		info := c.FilesToTransfer[index]
		stage, name, err := root.CreateTemp(stageDir, "member-", 0o600)
		if err != nil {
			return err
		}
		staged[i] = name
		hash := xxhash.New()
		n, copyErr := io.Copy(io.MultiWriter(stage, hash), &bundleContextReader{c, io.NewSectionReader(spool, s.offsets[i], info.Size)})
		closeErr := stage.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size || !bytes.Equal(hash.Sum(nil), info.Hash) {
			return fmt.Errorf("bundle member hash mismatch: %s", info.Name)
		}
	}
	// The owner cannot change accepted names during this commit. Validate their
	// portable collisions once, then recheck each actual filesystem destination.
	if _, _, err = validateReceiveMetadata(c.FilesToTransfer, c.EmptyFoldersToTransfer); err != nil {
		return err
	}
	for i, index := range s.request.Indices {
		if err = c.bundleContextErr(); err != nil {
			return err
		}
		info := c.FilesToTransfer[index]
		destination := path.Join(info.FolderRemote, info.Name)
		if err = root.MkdirAll(path.Dir(destination), 0o777); err != nil {
			return err
		}
		if err = recheckBundleDestination(root, info); err != nil {
			return err
		}
		var stage *os.File
		var openErr error
		if runtime.GOOS == "windows" {
			// FlushFileBuffers requires GENERIC_WRITE on the staged handle.
			stage, openErr = root.OpenFile(staged[i], os.O_RDWR, 0)
		} else {
			stage, openErr = root.Open(staged[i])
		}
		if openErr != nil {
			return openErr
		}
		chmodErr := stage.Chmod(info.Mode.Perm())
		syncErr := stage.Sync()
		closeErr := stage.Close()
		if chmodErr != nil {
			return chmodErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if !info.ModTime.IsZero() {
			if err = root.Chtimes(staged[i], info.ModTime, info.ModTime); err != nil {
				return err
			}
		}
		if err = root.PublishNew(staged[i], destination); err != nil {
			return fmt.Errorf("publish bundle member %q: %w", destination, err)
		}
		c.FilesHasFinished[index] = struct{}{}
		if len(c.bundles.identities) > index {
			if c.bundleCommitted == nil {
				c.bundleCommitted = make(map[string]FileInfo)
			}
			c.bundleCommitted[c.bundles.identities[index]] = info
		}
		c.markFileWrittenByReceiver(index)
		c.numberOfTransferredFiles++
		c.fmtPrintUpdate()
		log.Debugf("bundle member committed: %s (%s)", destination, hex.EncodeToString(info.Hash))
	}
	log.Debugf("bundle timing id=%d phase=verify-commit duration=%s payload=%d staged_peak=%d", s.request.ID, time.Since(started), s.size, s.size)
	return nil
}

func recheckBundleDestination(root *receivefs.Root, file FileInfo) error {
	destination := path.Join(file.FolderRemote, file.Name)
	if err := root.RejectSymlinkPath(destination); err != nil {
		return err
	}
	if _, err := root.Lstat(destination); !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("bundle destination appeared after preflight: %s", destination)
	}
	return nil
}

func (c *Client) newBundleProgress(indices []int, offsets []int64) func(int64, int) {
	// Render only original names and sizes. The payload ID and temporary filename
	// never become visible in normal output, including sender progress.
	var mu sync.Mutex
	sizes := make([]int64, len(indices))
	done := make([]int64, len(indices))
	current := 0
	for i, index := range indices {
		sizes[i] = c.FilesToTransfer[index].Size
	}
	show := func(i int) {
		c.setProgressBar(c.newDataUnitProgressBar(sizes[i], formatDescription(c.FilesToTransfer[indices[i]].Name), 0, nil))
	}
	show(0)
	return func(position int64, length int) {
		mu.Lock()
		defer mu.Unlock()
		for i := range indices {
			done[i] += max(int64(0), min(offsets[i]+sizes[i], position+int64(length))-max(offsets[i], position))
		}
		for current < len(indices) {
			c.barMu.RLock()
			bar := c.bar
			c.barMu.RUnlock()
			if bar != nil {
				_ = bar.Set64(done[current])
			}
			if done[current] != sizes[current] {
				break
			}
			c.finishProgress()
			current++
			if current < len(indices) {
				show(current)
			}
		}
	}
}

// A manifest index has no identity across attempts. Retain only accepted
// destinations whose original logical metadata still matches, then let normal
// preflight hash the on-disk destination again before skipping it.
func bundleFileIdentity(info FileInfo) string {
	data, _ := json.Marshal(struct {
		Path     string
		Size     int64
		Hash     []byte
		Mode     os.FileMode
		Modified string
		Temp     bool
		Symlink  string
	}{path.Join(info.FolderRemote, info.Name), info.Size, info.Hash, info.Mode, info.ModTime.UTC().Format("2006-01-02T15:04:05.999999999Z"), info.TempFile, info.Symlink})
	return string(data)
}

func (c *Client) restoreBundleDestinations() error {
	c.bundles.identities = make([]string, len(c.FilesToTransfer))
	c.FilesHasFinished = make(map[int]struct{})
	c.FilesToTransferCurrentNum = 0
	for index, info := range c.FilesToTransfer {
		identity := bundleFileIdentity(info)
		c.bundles.identities[index] = identity
		if accepted, ok := c.bundleCommitted[identity]; ok {
			c.FilesToTransfer[index].Name = accepted.Name
			c.FilesToTransfer[index].FolderRemote = accepted.FolderRemote
		}
	}
	_, _, err := validateReceiveMetadata(c.FilesToTransfer, c.EmptyFoldersToTransfer)
	return err
}

func (c *Client) retainCompletedReceiveIdentities() {
	for index := range c.FilesHasFinished {
		if index < 0 || index >= len(c.FilesToTransfer) || index >= len(c.bundles.identities) {
			continue
		}
		if c.bundleCommitted == nil {
			c.bundleCommitted = make(map[string]FileInfo)
		}
		c.bundleCommitted[c.bundles.identities[index]] = c.FilesToTransfer[index]
	}
}
