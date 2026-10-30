package croc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/schollz/croc/v11/src/compress"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/receivefs"
	"github.com/schollz/croc/v11/src/resume"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/utils"
)

type receiveFileState struct {
	index           int
	info            FileInfo
	path            string
	destPath        string
	tempPath        string
	small           bool
	file            *os.File
	size            int64
	chunkSize       int64
	chunkCount      int64
	journal         *resume.Journal
	journalPath     string
	mu              sync.RWMutex
	received        int64
	done            bool
	completing      bool
	compressed      bool
	lastCheckpoint  time.Time
	checkpointBytes int64
}

func (f *receiveFileState) missingRanges() []int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.journal.MissingRanges()
}

func (f *receiveFileState) completeCount() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.received
}

type batchReceiveState struct {
	client   *Client
	attempt  *transferAttemptState
	id       uint64
	ctx      context.Context
	cancelFn context.CancelFunc

	mu        sync.Mutex
	files     map[uint32]*receiveFileState
	order     []uint32
	failed    bool
	resetUsed bool
	aborted   bool

	verifySem     chan struct{}
	wg            sync.WaitGroup
	totalBytes    int64
	receivedBytes atomic.Int64
	started       time.Time
	perf          perfProbeState
}

func (c *Client) useParallelDataPlane() bool {
	return c.Options.ParallelData != "off" && c.peerParallelData &&
		!c.Options.Stdout && !c.Options.SendingText
}

func (c *Client) updateIfRecipientHasParallelBatch() error {
	if c.Options.IsSender || !c.parallelEnabled() {
		return nil
	}
	if c.currentParallelRecv() != nil {
		return nil
	}
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	maxFiles := c.parallelMaxFiles()
	limits := c.parallelLimits(c.parallelAvailableConnections())
	if c.Options.ParallelFiles == 0 && limits.ParallelFiles > 0 && limits.ParallelFiles < maxFiles {
		maxFiles = limits.ParallelFiles
	}
	if c.parallelFilesOverride > 0 && c.parallelFilesOverride < maxFiles {
		maxFiles = c.parallelFilesOverride
	}
	if maxFiles < 1 {
		maxFiles = 1
	}
	candidates := make([]int, 0, len(c.FilesToTransfer))
	hasUnprepared := false
	for i, info := range c.FilesToTransfer {
		if _, ok := c.FilesHasFinished[i]; ok {
			continue
		}
		if info.declined {
			continue
		}
		if c.progressiveHashActive() && !info.Prepared {
			hasUnprepared = true
			continue
		}
		candidates = append(candidates, i)
	}
	slices.SortStableFunc(candidates, func(a, b int) int {
		if c.FilesToTransfer[a].Size == c.FilesToTransfer[b].Size {
			return a - b
		}
		if c.FilesToTransfer[a].Size < c.FilesToTransfer[b].Size {
			return -1
		}
		return 1
	})

	var reqs []ParallelFileRequest
	var states []*receiveFileState
	var totalBytes int64
	var totalChunks int64
	waitingForPreparation := hasUnprepared
	for _, i := range candidates {
		if len(reqs) >= maxFiles {
			break
		}
		info := c.FilesToTransfer[i]
		decision, err := c.inspectParallelDestination(i, &info, root)
		if err != nil {
			closeReceiveStates(states)
			return err
		}
		switch decision {
		case parallelInspectUnchanged:
			c.numberOfUnchangedFiles++
			c.FilesHasFinished[i] = struct{}{}
			continue
		case parallelInspectDeclined:
			continue
		case parallelInspectWaiting:
			waitingForPreparation = true
			continue
		case parallelInspectMetadata:
			continue
		case parallelInspectData:
			// Continue below.
		}
		c.FilesToTransfer[i] = info
		chunkSize := c.parallelChunkSizeFor(info.Size)
		if c.parallelChunkOverride > 0 && c.parallelChunkOverride < chunkSize {
			chunkSize = c.parallelChunkOverride
		}
		if !validParallelChunkSize(chunkSize, info.Size) {
			chunkSize = defaultParallelChunkSize(info.Size)
		}
		state, complete, err := c.openParallelReceiveFile(i, info, chunkSize)
		if err != nil {
			closeReceiveStates(states)
			return err
		}
		if complete {
			c.FilesHasFinished[i] = struct{}{}
			c.numberOfUnchangedFiles++
			continue
		}
		ranges := state.missingRanges()
		if len(ranges) == 0 {
			state.close()
			c.FilesHasFinished[i] = struct{}{}
			c.numberOfUnchangedFiles++
			continue
		}
		c.countParallelTransferredFile(i)
		req := ParallelFileRequest{
			Index:     i,
			ChunkSize: chunkSize,
			Ranges:    ranges,
			Resume:    c.parallelResumeEnabled(),
		}
		if len(ranges)/2 > parallelMaxSegments {
			req.Bitmap = rangesToBitmap(ranges, parallelChunkCount(info.Size, chunkSize))
			req.Ranges = nil
		}
		chunkCount := int64(countRequestedChunks(ranges))
		if len(states) > 0 && info.Size > parallelSmallFileThreshold {
			state.close()
			break
		}
		if len(states) > 0 {
			if totalBytes+info.Size > 64<<20 || totalChunks+chunkCount > parallelMaxBatchChunks {
				state.close()
				break
			}
		}
		reqs = append(reqs, req)
		states = append(states, state)
		totalBytes += parallelRangesBytes(ranges, info.Size, chunkSize)
		totalChunks += chunkCount
		if info.Size > parallelSmallFileThreshold {
			break
		}
	}
	if len(reqs) == 0 {
		closeReceiveStates(states)
		if waitingForPreparation {
			return nil
		}
		allFinished := true
		for i := range c.FilesToTransfer {
			if _, ok := c.FilesHasFinished[i]; ok {
				continue
			}
			if c.FilesToTransfer[i].declined {
				continue
			}
			allFinished = false
			break
		}
		if allFinished {
			c.FilesToTransferCurrentNum = max(0, len(c.FilesToTransfer)-1)
			return c.recipientGetFileReady(true)
		}
		return nil
	}
	return c.startParallelReceiveBatch(reqs, states, totalBytes)
}

type parallelInspectDecision int

const (
	parallelInspectData parallelInspectDecision = iota
	parallelInspectUnchanged
	parallelInspectDeclined
	parallelInspectMetadata
	parallelInspectWaiting
)

func (c *Client) inspectParallelDestination(index int, info *FileInfo, root *receivefs.Root) (parallelInspectDecision, error) {
	destination := path.Join(info.FolderRemote, info.Name)
	recipientFileInfo, err := root.Lstat(destination)
	if err != nil && !os.IsNotExist(err) {
		return parallelInspectData, err
	}
	exists := err == nil
	var fileHash []byte
	if exists && info.Size > 0 && info.Symlink == "" && recipientFileInfo.Mode().IsRegular() && recipientFileInfo.Size() == info.Size {
		fileHash, err = receiveFileHash(c.clientContext(), destination, c.Options.HashAlgorithm, !c.Options.SendingText)
		if err != nil {
			return parallelInspectData, fmt.Errorf("hash existing destination %q: %w", destination, err)
		}
	}
	hashesEqual := exists && info.Size > 0 && info.Symlink == "" &&
		recipientFileInfo.Mode().IsRegular() && recipientFileInfo.Size() == info.Size &&
		bytes.Equal(fileHash, info.Hash)
	if hashesEqual && c.progressiveHashActive() {
		known, exactMatch, pending := c.exactHashDecision(index)
		switch {
		case known:
			hashesEqual = exactMatch
		case pending:
			return parallelInspectWaiting, nil
		default:
			if err := c.beginExactHash(index, destination); err != nil {
				return parallelInspectData, err
			}
			return parallelInspectWaiting, nil
		}
	}
	if hashesEqual {
		if !info.ModTime.IsZero() {
			if err := root.Chtimes(destination, info.ModTime, info.ModTime); err != nil {
				log.Warnf("chtimes %v: %v", info.ModTime, err)
			}
		}
		return parallelInspectUnchanged, nil
	}
	if exists && c.Options.Rename {
		newName := utils.UnusedFilename(info.FolderRemote, info.Name)
		output, colorEnabled := termui.Output(os.Stderr)
		fmt.Fprintf(output, "Receiving %s as %s\n", quotedFilename(info.Name, colorEnabled), quotedFilename(newName, colorEnabled))
		info.Name = newName
		c.FilesToTransfer[index].Name = newName
		exists = false
	}
	if exists && !c.Options.Overwrite && !c.Options.Rename {
		overwrite, promptErr := askReceiveOverwrite(c.clientContext(), *info, false)
		if promptErr != nil {
			return parallelInspectDeclined, promptErr
		}
		if !overwrite {
			c.FilesToTransfer[index].declined = true
			return parallelInspectDeclined, nil
		}
	}
	if info.Size == 0 || info.Symlink != "" {
		if err := c.createEmptyFileV2(*info, index); err != nil {
			return parallelInspectData, err
		}
		c.FilesHasFinished[index] = struct{}{}
		c.countParallelTransferredFile(index)
		return parallelInspectMetadata, nil
	}
	return parallelInspectData, nil
}

func (c *Client) countParallelTransferredFile(index int) {
	if c.parallelCountedFiles == nil {
		c.parallelCountedFiles = make(map[int]struct{})
	}
	if _, ok := c.parallelCountedFiles[index]; ok {
		return
	}
	c.parallelCountedFiles[index] = struct{}{}
	c.numberOfTransferredFiles++
}

func (c *Client) createEmptyFileV2(fileInfo FileInfo, i int) error {
	folderRemote, pathToFile, err := normalizeReceiveFilePath(fileInfo.FolderRemote, fileInfo.Name)
	if err != nil {
		return err
	}
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	if err := root.MkdirAll(folderRemote, os.ModePerm); err != nil {
		return err
	}
	if fileInfo.Symlink != "" {
		if err := validateReceiveSymlinkTarget(folderRemote, fileInfo.Symlink); err != nil {
			return err
		}
		if _, err := root.Lstat(pathToFile); err == nil {
			if err := root.Remove(pathToFile); err != nil {
				return err
			}
		}
		return root.Symlink(fileInfo.Symlink, pathToFile)
	}
	file, err := root.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	if err := file.Chmod(fileInfo.Mode.Perm()); err != nil {
		log.Debugf("chmod %s: %v", pathToFile, err)
	}
	return file.Close()
}

func closeReceiveStates(states []*receiveFileState) {
	for _, state := range states {
		state.close()
	}
}

func (c *Client) openParallelReceiveFile(index int, info FileInfo, chunkSize int64) (*receiveFileState, bool, error) {
	folderRemote, pathToFile, err := normalizeReceiveFilePath(info.FolderRemote, info.Name)
	if err != nil {
		return nil, false, err
	}
	c.FilesToTransfer[index].FolderRemote = folderRemote
	c.FilesToTransfer[index].Name = path.Base(pathToFile)
	root, err := c.receiveFilesystem()
	if err != nil {
		return nil, false, err
	}
	folderForFile, _ := path.Split(pathToFile)
	folderForFile = path.Clean(folderForFile)
	if folderForFile != "." && folderForFile != "" {
		if err := root.MkdirAll(folderForFile, os.ModePerm); err != nil {
			return nil, false, err
		}
	}
	small := info.Size > 0 && info.Size <= parallelSmallFileThreshold
	meta := resume.Metadata{
		FileSize:      info.Size,
		ModTime:       info.ModTime.UnixNano(),
		SourceHash:    append([]byte(nil), info.Hash...),
		HashAlgorithm: c.Options.HashAlgorithm,
		ChunkSize:     chunkSize,
		ChunkCount:    parallelChunkCount(info.Size, chunkSize),
		Destination:   pathToFile,
		StateVersion:  1,
	}
	journalPath := ""
	var journal *resume.Journal
	if c.parallelResumeEnabled() {
		journalPath = pathToFile + ".crocstate"
		var usable bool
		journal, usable, _ = c.loadReceiveJournal(root, journalPath, meta)
		if !usable {
			// Preserve a foreign file that happens to use the sidecar name.
			journalPath = ""
			journal = nil
		}
	}

	writePath := pathToFile
	tempPath := ""
	created := false
	var file *os.File
	if small {
		// Small files are written to a hidden temporary file and renamed only
		// after the whole-file hash matches, so a partial download never
		// replaces the destination.
		if journal != nil {
			tempPath = journal.Metadata().TempPath
			expectedDir := path.Dir(pathToFile)
			expectedPrefix := "." + path.Base(pathToFile) + ".crocpart-"
			if tempPath == "" || path.Dir(tempPath) != expectedDir || !strings.HasPrefix(path.Base(tempPath), expectedPrefix) {
				_ = root.Remove(journalPath)
				journal = nil
				tempPath = ""
			} else if stat, statErr := root.Lstat(tempPath); statErr != nil || !stat.Mode().IsRegular() || stat.Size() != info.Size {
				log.Debugf("resume: temp %s unusable: %v", tempPath, statErr)
				_ = root.Remove(journalPath)
				journal = nil
				tempPath = ""
			}
		}
		if journal != nil {
			file, err = root.OpenFile(tempPath, os.O_RDWR, 0o666)
			if err != nil {
				_ = root.Remove(journalPath)
				journal = nil
				tempPath = ""
				file = nil
			}
		}
		if file == nil {
			file, tempPath, err = root.CreateTemp(path.Dir(pathToFile), "."+path.Base(pathToFile)+".crocpart-", 0o600)
			if err != nil {
				return nil, false, fmt.Errorf("could not create temporary file for %s: %w", pathToFile, err)
			}
			if err := file.Chmod(info.Mode.Perm()); err != nil {
				log.Debugf("chmod %s: %v", tempPath, err)
			}
			if err := file.Truncate(info.Size); err != nil {
				file.Close()
				return nil, false, fmt.Errorf("could not truncate %s: %w", tempPath, err)
			}
		}
		writePath = tempPath
		meta.TempPath = tempPath
	} else {
		_, statErr := root.Lstat(pathToFile)
		created = os.IsNotExist(statErr)
		file, err = root.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			return nil, false, fmt.Errorf("could not open %s: %w", pathToFile, err)
		}
		if created {
			if err := file.Chmod(info.Mode.Perm()); err != nil {
				log.Debugf("chmod %s: %v", pathToFile, err)
			}
		}
		stat, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return nil, false, statErr
		}
		if stat.Size() != info.Size {
			if journal != nil {
				_ = root.Remove(journalPath)
				journal = nil
			}
			if err := file.Truncate(info.Size); err != nil {
				file.Close()
				return nil, false, fmt.Errorf("could not truncate %s: %w", pathToFile, err)
			}
		}
	}
	state := &receiveFileState{
		index:       index,
		info:        info,
		path:        writePath,
		destPath:    pathToFile,
		tempPath:    tempPath,
		small:       small,
		file:        file,
		size:        info.Size,
		chunkSize:   chunkSize,
		chunkCount:  meta.ChunkCount,
		journalPath: journalPath,
	}
	if journal != nil {
		state.journal = journal
		if cleared, verifyErr := journal.VerifyCompleted(file, c.resumeVerifyMode()); verifyErr != nil {
			state.journal = nil
			_ = root.Remove(journalPath)
			_ = file.Truncate(info.Size)
		} else if cleared > 0 {
			log.Debugf("resume: cleared %d invalid chunks for %s", cleared, writePath)
		}
	}
	if state.journal == nil {
		state.journal = resume.New(meta)
	}
	state.received = state.journal.Count()
	log.Debugf("resume: %s journal=%v received=%d/%d", writePath, journal != nil, state.received, state.chunkCount)
	if state.received > 0 {
		c.statsAddResumed(state.received * chunkSize)
	}
	if state.received >= state.chunkCount {
		hash, hashErr := receiveFileHash(c.clientContext(), writePath, c.Options.HashAlgorithm, false)
		if hashErr == nil && bytes.Equal(hash, info.Hash) {
			if small {
				if err := finalizeSmallReceiveFile(root, state); err != nil {
					return nil, false, err
				}
			} else if err := state.closeFile(); err != nil {
				return nil, false, err
			}
			if state.journalPath != "" {
				_ = root.Remove(state.journalPath)
			}
			return nil, true, nil
		}
		state.journal.Reset()
		state.received = 0
	}
	state.lastCheckpoint = time.Now()
	if err := c.checkpointReceiveFileState(state); err != nil {
		file.Close()
		return nil, false, err
	}
	c.markFileWrittenByReceiver(index)
	return state, false, nil
}

func finalizeSmallReceiveFile(root *receivefs.Root, state *receiveFileState) error {
	if err := state.closeFile(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Rename does not replace an existing destination on Windows.
		if err := root.Remove(state.destPath); err != nil {
			if _, statErr := root.Lstat(state.destPath); statErr == nil {
				return fmt.Errorf("remove old destination %s: %w", state.destPath, err)
			}
		}
	}
	if err := root.Rename(state.tempPath, state.destPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", state.tempPath, state.destPath, err)
	}
	state.path = state.destPath
	return nil
}

func (c *Client) resumeVerifyMode() string {
	switch c.Options.ResumeVerify {
	case "sample", "none":
		return c.Options.ResumeVerify
	default:
		return "full"
	}
}

// loadReceiveJournal returns (journal, usable, error). usable is false when
// the sidecar path exists but is not a croc journal; callers must leave that
// file untouched and disable persistent resume for this destination.
func (c *Client) loadReceiveJournal(root *receivefs.Root, journalPath string, meta resume.Metadata) (*resume.Journal, bool, error) {
	file, err := root.Open(journalPath)
	if err != nil {
		log.Debugf("resume: open %s: %v", journalPath, err)
		if os.IsNotExist(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 64<<20))
	closeErr := file.Close()
	if readErr != nil {
		return nil, false, readErr
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	journal, err := resume.Parse(data)
	if err != nil {
		log.Debugf("resume: ignoring unreadable journal %s: %v", journalPath, err)
		return nil, false, nil
	}
	if !journal.Metadata().Matches(meta) {
		log.Debugf("resume: metadata mismatch for %s", journalPath)
		_ = root.Remove(journalPath)
		return nil, true, nil
	}
	return journal, true, nil
}

func (c *Client) checkpointReceiveFileState(state *receiveFileState) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	return c.checkpointReceiveFileLocked(state)
}

func (c *Client) checkpointReceiveFileLocked(state *receiveFileState) error {
	if state.file == nil || state.journal == nil || state.journalPath == "" {
		return nil
	}
	if err := state.file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", state.path, err)
	}
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	if err := root.WriteFileAtomic(state.journalPath, state.journal.Marshal(), 0o600); err != nil {
		return fmt.Errorf("write resume journal for %s: %w", state.path, err)
	}
	state.lastCheckpoint = time.Now()
	state.checkpointBytes = state.received * state.chunkSize
	return nil
}

func (state *receiveFileState) closeFile() error {
	if state.file == nil {
		return nil
	}
	err := state.file.Close()
	state.file = nil
	return err
}

func (state *receiveFileState) close() {
	state.mu.Lock()
	defer state.mu.Unlock()
	_ = state.closeFile()
}

func (state *receiveFileState) markComplete(chunk int64, hash uint64) (bool, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.done || state.completing || state.journal == nil {
		return false, false
	}
	if state.journal.IsComplete(chunk) {
		return false, false
	}
	state.journal.MarkComplete(chunk, hash)
	state.received++
	complete := state.received >= state.chunkCount
	if complete {
		state.completing = true
	}
	return true, complete
}

func (state *receiveFileState) maybeCheckpoint(c *Client) error {
	state.mu.Lock()
	if state.done || state.completing || state.file == nil || state.journalPath == "" {
		state.mu.Unlock()
		return nil
	}
	due := time.Since(state.lastCheckpoint) >= time.Second ||
		(state.received*state.chunkSize)-state.checkpointBytes >= 32<<20
	if !due {
		state.mu.Unlock()
		return nil
	}
	err := c.checkpointReceiveFileLocked(state)
	state.mu.Unlock()
	return err
}

// parallelRangesBytes returns the byte count requested by v2 ranges, whose
// start field is a chunk index (unlike the legacy byte-offset format).
func parallelRangesBytes(ranges []int64, fileSize, chunkSize int64) int64 {
	var total int64
	for i := 1; i+1 < len(ranges); i += 2 {
		start, count := ranges[i], ranges[i+1]
		offset := start * chunkSize
		end := min((start+count)*chunkSize, fileSize)
		if end > offset {
			total += end - offset
		}
	}
	return total
}

func rangesToBitmap(ranges []int64, chunkCount int64) []byte {
	bitmap := make([]byte, (chunkCount+7)/8)
	for i := 1; i+1 < len(ranges); i += 2 {
		start, count := ranges[i], ranges[i+1]
		for chunk := start; chunk < start+count && chunk < chunkCount; chunk++ {
			bitmap[chunk/8] |= 1 << uint(chunk%8)
		}
	}
	return bitmap
}

func (c *Client) startParallelReceiveBatch(reqs []ParallelFileRequest, states []*receiveFileState, totalBytes int64) error {
	ctx, cancel := context.WithCancel(c.clientContext())
	batch := &batchReceiveState{
		client:     c,
		attempt:    c.currentAttempt,
		id:         c.nextParallelBatchID(),
		ctx:        ctx,
		cancelFn:   cancel,
		files:      make(map[uint32]*receiveFileState, len(states)),
		verifySem:  make(chan struct{}, 2),
		totalBytes: totalBytes,
		started:    time.Now(),
	}
	batch.perf.init()
	for _, state := range states {
		batch.files[uint32(state.index)] = state
		batch.order = append(batch.order, uint32(state.index))
	}
	return c.sendParallelReceiveReady(batch, reqs, totalBytes)
}

func (c *Client) sendParallelReceiveReady(batch *batchReceiveState, reqs []ParallelFileRequest, totalBytes int64) error {
	body, err := json.Marshal(ParallelBatchReady{BatchID: batch.id, Files: reqs, ReconnectVersion: c.reconnectVersion})
	if err != nil {
		closeReceiveStates(receiveStates(batch))
		return err
	}
	c.setParallelRecv(batch)
	c.startParallelProgress(totalBytes, len(reqs))
	go c.runParallelReceiveSampler(batch)
	if err := c.sendParallelMessage(message.Message{Type: message.TypeParallelBatchReady, Bytes: body}); err != nil {
		batch.abort(nil)
		return transferDisconnectError{err: err}
	}
	c.updateLifecycle(func(state *transferLifecycle) { state.RecipientRequested = true })
	c.markTransferStarted()
	batch.perf.send(c)
	log.Debugf("parallel: receive batch %d files=%d bytes=%d", batch.id, len(reqs), totalBytes)
	return nil
}

func receiveStates(batch *batchReceiveState) []*receiveFileState {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	states := make([]*receiveFileState, 0, len(batch.files))
	for _, state := range batch.files {
		states = append(states, state)
	}
	return states
}

func (c *Client) handleDataFrameV2(plain []byte, attempt *transferAttemptState) error {
	frame, err := decodeDataFrameV2(plain, parallelMaxChunkSize+64<<10)
	if err != nil {
		return err
	}
	batch := c.currentParallelRecv()
	if batch == nil {
		c.parallelMu.Lock()
		last := c.parallelLastBatchID
		c.parallelMu.Unlock()
		if frame.batchID == last {
			return nil
		}
		return fmt.Errorf("parallel data frame before batch ready")
	}
	if frame.batchID != batch.id {
		c.parallelMu.Lock()
		last := c.parallelLastBatchID
		c.parallelMu.Unlock()
		if frame.batchID == last {
			return nil
		}
		return fmt.Errorf("parallel data frame batch %d does not match %d", frame.batchID, batch.id)
	}
	batch.mu.Lock()
	state := batch.files[frame.fileID]
	batch.mu.Unlock()
	if state == nil {
		return fmt.Errorf("parallel data frame for unknown file %d", frame.fileID)
	}
	state.mu.RLock()
	done := state.done
	completing := state.completing
	chunkSize := state.chunkSize
	size := state.size
	file := state.file
	state.mu.RUnlock()
	if done || completing {
		return nil
	}
	if file == nil {
		return fmt.Errorf("parallel receive file is closed")
	}
	payload := frame.payload
	if frame.flags&parallelFlagCompressed != 0 {
		payload, err = compress.DecompressTo(nil, payload, chunkSize)
		if err != nil {
			return fmt.Errorf("decompress parallel chunk: %w", err)
		}
	}
	if int64(len(payload)) == 0 {
		return fmt.Errorf("empty parallel chunk")
	}
	if frame.offset < 0 || frame.offset >= size {
		return fmt.Errorf("parallel chunk offset %d out of range", frame.offset)
	}
	if frame.offset%chunkSize != 0 {
		return fmt.Errorf("parallel chunk offset %d is not aligned to %d", frame.offset, chunkSize)
	}
	final := frame.flags&parallelFlagFinal != 0
	if final {
		if frame.offset+int64(len(payload)) != size {
			return fmt.Errorf("final parallel chunk ends at %d, want %d", frame.offset+int64(len(payload)), size)
		}
	} else if int64(len(payload)) != chunkSize || frame.offset+chunkSize > size {
		return fmt.Errorf("parallel chunk length %d does not match chunk size %d", len(payload), chunkSize)
	}
	state.mu.RLock()
	if state.done || state.completing || state.file == nil {
		state.mu.RUnlock()
		return nil
	}
	if _, err := state.file.WriteAt(payload, frame.offset); err != nil {
		state.mu.RUnlock()
		return err
	}
	state.mu.RUnlock()
	chunk := frame.offset / chunkSize
	added, complete := state.markComplete(chunk, xxhash.Sum64(payload))
	if added {
		c.addParallelProgress(int64(len(payload)))
		batch.receivedBytes.Add(int64(len(payload)))
		if err := state.maybeCheckpoint(c); err != nil {
			return err
		}
	}
	if complete && added {
		batch.startVerify(state)
	}
	return nil
}

func (batch *batchReceiveState) startVerify(state *receiveFileState) {
	batch.mu.Lock()
	if batch.aborted {
		batch.mu.Unlock()
		return
	}
	batch.mu.Unlock()
	batch.wg.Go(func() {
		select {
		case batch.verifySem <- struct{}{}:
		case <-batch.ctx.Done():
			return
		}
		defer func() { <-batch.verifySem }()
		if err := batch.verifyFile(state); err != nil && batch.ctx.Err() == nil {
			if batch.attempt != nil {
				batch.attempt.report(err)
			}
			batch.abort(nil)
		}
	})
}

func (batch *batchReceiveState) verifyFile(state *receiveFileState) error {
	c := batch.client
	state.mu.Lock()
	if state.done || state.file == nil {
		state.mu.Unlock()
		return nil
	}
	if err := state.file.Sync(); err != nil {
		state.mu.Unlock()
		return err
	}
	state.mu.Unlock()

	hash, err := receiveFileHash(batch.ctx, state.path, c.Options.HashAlgorithm, false)
	if err != nil {
		return fmt.Errorf("hash received file %s: %w", state.path, err)
	}
	if !bytes.Equal(hash, state.info.Hash) {
		state.mu.Lock()
		state.journal.Reset()
		state.received = 0
		state.completing = false
		state.mu.Unlock()
		c.statsAddRetry()
		if err := c.checkpointReceiveFileState(state); err != nil {
			return err
		}
		body, _ := json.Marshal(ParallelFileDone{
			BatchID: batch.id,
			Index:   state.index,
			Status:  parallelStatusHashMismatch,
		})
		return c.sendParallelMessage(message.Message{Type: message.TypeParallelFileDone, Bytes: body})
	}

	root, rootErr := c.receiveFilesystem()
	if rootErr != nil {
		return rootErr
	}
	if state.small {
		if err := finalizeSmallReceiveFile(root, state); err != nil {
			return err
		}
	} else {
		state.mu.Lock()
		err := state.closeFile()
		state.mu.Unlock()
		if err != nil {
			return err
		}
	}
	state.mu.Lock()
	state.done = true
	state.completing = false
	state.mu.Unlock()
	finalPath := state.path
	if state.small {
		finalPath = state.destPath
	}
	if !state.info.ModTime.IsZero() {
		if err := root.Chtimes(finalPath, state.info.ModTime, state.info.ModTime); err != nil {
			log.Debugf("chtimes %s: %v", finalPath, err)
		}
	}
	if state.journalPath != "" {
		if err := root.Remove(state.journalPath); err != nil && !os.IsNotExist(err) {
			log.Debugf("remove resume journal %s: %v", state.journalPath, err)
		}
	}
	body, _ := json.Marshal(ParallelFileDone{
		BatchID: batch.id,
		Index:   state.index,
		Status:  parallelStatusOK,
		Bytes:   state.size,
		Hash:    hash,
	})
	return c.sendParallelMessage(message.Message{Type: message.TypeParallelFileDone, Bytes: body})
}

func (batch *batchReceiveState) abort(err error) {
	batch.mu.Lock()
	if batch.aborted {
		batch.mu.Unlock()
		return
	}
	batch.aborted = true
	batch.failed = true
	batch.mu.Unlock()
	if batch.cancelFn != nil {
		batch.cancelFn()
	}
	for _, state := range receiveStates(batch) {
		_ = batch.client.checkpointReceiveFileState(state)
		state.close()
	}
	batch.client.clearParallelRecv(batch)
	if err != nil && batch.attempt != nil {
		batch.attempt.report(err)
	}
}

func (c *Client) processParallelBatchDone(m message.Message) error {
	if c.Options.IsSender {
		return fmt.Errorf("unexpected parallel batch done")
	}
	batch := c.currentParallelRecv()
	if batch == nil {
		return fmt.Errorf("parallel batch done without an active batch")
	}
	var done ParallelBatchDone
	if err := json.Unmarshal(m.Bytes, &done); err != nil {
		return fmt.Errorf("decode parallel batch done: %w", err)
	}
	if done.BatchID != batch.id {
		return fmt.Errorf("parallel batch done %d does not match %d", done.BatchID, batch.id)
	}
	switch done.Status {
	case parallelStatusOK:
	case parallelStatusError:
		batch.abort(fmt.Errorf("sender reported a batch error"))
		return nil
	default:
		return fmt.Errorf("unknown parallel batch status %q", done.Status)
	}
	batch.mu.Lock()
	for _, state := range batch.files {
		state.mu.RLock()
		received := state.done
		state.mu.RUnlock()
		if !received {
			batch.mu.Unlock()
			return fmt.Errorf("parallel batch done before file %d completed", state.index)
		}
	}
	batch.aborted = true
	batch.mu.Unlock()
	batch.cancelFn()
	for _, state := range receiveStates(batch) {
		c.FilesHasFinished[state.index] = struct{}{}
		state.close()
	}
	c.clearParallelRecv(batch)
	c.parallelMu.Lock()
	c.parallelLastBatchID = batch.id
	c.parallelMu.Unlock()
	c.updateLifecycle(func(state *transferLifecycle) { state.RecipientRequested = false })
	c.finishParallelProgress(len(batch.order))
	return nil
}

func (c *Client) processParallelBatchReset(m message.Message) error {
	if c.Options.IsSender {
		return fmt.Errorf("unexpected parallel batch reset")
	}
	batch := c.currentParallelRecv()
	if batch == nil {
		return fmt.Errorf("parallel batch reset without an active batch")
	}
	var reset ParallelBatchReset
	if err := json.Unmarshal(m.Bytes, &reset); err != nil {
		return fmt.Errorf("decode parallel batch reset: %w", err)
	}
	if reset.BatchID != batch.id {
		return fmt.Errorf("parallel batch reset %d does not match %d", reset.BatchID, batch.id)
	}
	batch.mu.Lock()
	alreadyReset := batch.resetUsed
	received := batch.receivedBytes.Load()
	batch.resetUsed = true
	batch.mu.Unlock()
	if alreadyReset {
		return fmt.Errorf("parallel batch reset used more than once")
	}
	if received > 0 {
		return fmt.Errorf("parallel batch reset arrived after data")
	}
	if reset.MaxChunkSize < 0 || reset.MaxChunkSize > parallelMaxChunkSize {
		return fmt.Errorf("invalid reset chunk limit %d", reset.MaxChunkSize)
	}
	if reset.MaxChunkSize > 0 {
		c.parallelChunkOverride = reset.MaxChunkSize
	}
	if reset.MaxBatchFiles > 0 && (c.parallelFilesOverride == 0 || reset.MaxBatchFiles < c.parallelFilesOverride) {
		c.parallelFilesOverride = reset.MaxBatchFiles
	}
	for _, state := range receiveStates(batch) {
		if root, err := c.receiveFilesystem(); err == nil {
			if state.journalPath != "" {
				_ = root.Remove(state.journalPath)
			}
			if state.tempPath != "" {
				_ = root.Remove(state.tempPath)
			}
		}
		state.close()
	}
	batch.mu.Lock()
	batch.aborted = true
	batch.mu.Unlock()
	batch.cancelFn()
	c.clearParallelRecv(batch)
	c.updateLifecycle(func(state *transferLifecycle) { state.RecipientRequested = false })
	return nil
}
