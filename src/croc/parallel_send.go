package croc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/compress"
	"github.com/schollz/croc/v11/src/crypt"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
)

type fileSendState struct {
	index      int
	info       FileInfo
	file       *os.File
	size       int64
	chunkSize  int64
	compressed bool
	ranges     []int64
	fullRanges []int64
	cursor     int
	step       int64
	remaining  atomic.Int64
	chunkCount int64
	mu         sync.Mutex
	closed     bool
}

func newFileSendState(index int, info FileInfo, chunkSize int64, ranges []int64, compressed bool) (*fileSendState, error) {
	file, err := os.Open(sourceFilePath(info))
	if err != nil {
		return nil, err
	}
	count := parallelChunkCount(info.Size, chunkSize)
	state := &fileSendState{
		index:      index,
		info:       info,
		file:       file,
		size:       info.Size,
		chunkSize:  chunkSize,
		compressed: compressed,
		ranges:     append([]int64(nil), ranges...),
		fullRanges: []int64{chunkSize, 0, count},
		cursor:     1,
		chunkCount: count,
	}
	state.remaining.Store(countRequestedChunks(ranges))
	return state, nil
}

func countRequestedChunks(ranges []int64) int64 {
	var count int64
	for i := 2; i < len(ranges); i += 2 {
		if ranges[i] > 0 {
			count += ranges[i]
		}
	}
	return count
}

func (f *fileSendState) claimChunk() (int64, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, 0, false
	}
	for f.cursor+1 < len(f.ranges) {
		start := f.ranges[f.cursor]
		count := f.ranges[f.cursor+1]
		if f.step < count {
			offset := (start + f.step) * f.chunkSize
			length := f.chunkSize
			if remaining := f.size - offset; remaining < length {
				length = remaining
			}
			if length <= 0 || offset < 0 || offset >= f.size {
				return 0, 0, false
			}
			f.step++
			return offset, int(length), true
		}
		f.cursor += 2
		f.step = 0
	}
	return 0, 0, false
}

func (f *fileSendState) completeChunk() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remaining.Add(-1) > 0 || f.file == nil {
		return
	}
	if err := f.file.Close(); err != nil && !errorsIsClosed(err) {
		log.Debugf("closing sent file: %v", err)
	}
	f.file = nil
	f.closed = true
}

func errorsIsClosed(err error) bool {
	return errors.Is(err, os.ErrClosed)
}

func (f *fileSendState) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file != nil {
		_ = f.file.Close()
		f.file = nil
	}
	f.closed = true
}

func (f *fileSendState) resetForRetry() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		file, err := os.Open(sourceFilePath(f.info))
		if err != nil {
			return err
		}
		f.file = file
	}
	f.ranges = append(f.ranges[:0], f.fullRanges...)
	f.cursor = 1
	f.step = 0
	f.remaining.Store(f.chunkCount)
	f.closed = false
	return nil
}

type outFrame struct {
	wire []byte
	n    int64
	task chunkTask
}

type batchSendState struct {
	client           *Client
	attempt          *transferAttemptState
	id               uint64
	ctx              context.Context
	cancelFn         context.CancelFunc
	queue            *parallelChunkQueue
	files            map[uint32]*fileSendState
	order            []uint32
	inFlight         int
	allowLateWorkers bool
	started          time.Time
	perf             perfProbeState
	sentBytes        atomic.Int64
	blockedNanos     atomic.Int64

	wg         sync.WaitGroup
	mu         sync.Mutex
	workers    map[*comm.Comm]struct{}
	done       map[uint32]string
	results    map[uint32]ParallelFileDone
	retries    map[uint32]int
	closed     bool
	finished   bool
	total      int
	maxWorkers int
}

func (c *Client) processParallelBatchReady(m message.Message, attempt *transferAttemptState) error {
	if !c.Options.IsSender || !c.parallelEnabled() {
		return fmt.Errorf("unexpected parallel batch ready")
	}
	if c.currentParallelSend() != nil {
		return fmt.Errorf("parallel batch already active")
	}
	if len(m.Bytes) > 8<<20 {
		return fmt.Errorf("parallel batch request too large")
	}
	var ready ParallelBatchReady
	if err := json.Unmarshal(m.Bytes, &ready); err != nil {
		return fmt.Errorf("decode parallel batch ready: %w", err)
	}
	if err := ready.validate(c.FilesToTransfer); err != nil {
		return err
	}
	c.peerReconnectVersion = ready.ReconnectVersion
	maxChunk := c.effectiveMaxChunkSize()
	for _, req := range ready.Files {
		if req.ChunkSize > maxChunk {
			reset := ParallelBatchReset{
				BatchID:       ready.BatchID,
				Reason:        "chunk size exceeds sender limit",
				MaxChunkSize:  maxChunk,
				MaxBatchFiles: c.parallelMaxFiles(),
			}
			body, _ := json.Marshal(reset)
			if err := c.sendParallelMessage(message.Message{Type: message.TypeParallelBatchReset, Bytes: body}); err != nil {
				return err
			}
			return nil
		}
	}
	if err := c.startParallelSendBatch(attempt, &ready); err != nil {
		return err
	}
	c.updateLifecycle(func(state *transferLifecycle) { state.FileTransferred = true })
	c.markTransferStarted()
	return nil
}

func (c *Client) startParallelSendBatch(attempt *transferAttemptState, ready *ParallelBatchReady) error {
	ctx, cancel := context.WithCancel(c.clientContext())
	batch := &batchSendState{
		client:           c,
		attempt:          attempt,
		id:               ready.BatchID,
		ctx:              ctx,
		cancelFn:         cancel,
		files:            make(map[uint32]*fileSendState, len(ready.Files)),
		order:            make([]uint32, 0, len(ready.Files)),
		done:             make(map[uint32]string, len(ready.Files)),
		results:          make(map[uint32]ParallelFileDone, len(ready.Files)),
		retries:          make(map[uint32]int, len(ready.Files)),
		workers:          make(map[*comm.Comm]struct{}),
		total:            len(ready.Files),
		allowLateWorkers: c.peerStagedTransport,
		started:          time.Now(),
	}
	batch.perf.init()
	var queueFiles []*fileSendState
	var totalBytes int64
	for _, req := range ready.Files {
		info := c.FilesToTransfer[req.Index]
		if err := c.validateSourceUnchanged(req.Index); err != nil {
			cancel()
			for _, state := range batch.files {
				state.close()
			}
			return err
		}
		state, err := newFileSendState(req.Index, info, req.ChunkSize, req.Ranges, info.IsCompressed && !c.Options.NoCompress)
		if err != nil {
			cancel()
			for _, opened := range batch.files {
				opened.close()
			}
			return err
		}
		batch.files[uint32(req.Index)] = state
		batch.order = append(batch.order, uint32(req.Index))
		queueFiles = append(queueFiles, state)
		totalBytes += parallelRangesBytes(req.Ranges, info.Size, req.ChunkSize)
	}
	batch.queue = newParallelChunkQueue(queueFiles)
	limits := c.parallelLimits(c.parallelAvailableConnections())
	batch.inFlight = limits.InFlight
	if batch.inFlight < 1 {
		batch.inFlight = 1
	}
	batch.maxWorkers = limits.Connections
	if batch.maxWorkers < 1 {
		batch.maxWorkers = 1
	}
	c.setParallelSend(batch)
	c.startParallelProgress(totalBytes, len(ready.Files))
	c.statsPeakConnections(limits.Connections)
	log.Debugf("parallel: send batch %d files=%d bytes=%d chunk=%d inflight=%d", batch.id, len(ready.Files), totalBytes, limits.ChunkSize, batch.inFlight)

	connections := c.connectionsSnapshot()
	started := 0
	for i := 1; i < len(connections) && started < limits.Connections; i++ {
		if connections[i] == nil {
			continue
		}
		c.startParallelWorker(connections[i])
		started++
	}
	if started == 0 {
		batch.cancel(fmt.Errorf("no data connections available for parallel transfer"))
		return fmt.Errorf("no data connections available for parallel transfer")
	}
	go c.runParallelSendSampler(batch)
	return nil
}

func (c *Client) startParallelWorker(conn *comm.Comm) {
	batch := c.currentParallelSend()
	if batch == nil || conn == nil {
		return
	}
	batch.mu.Lock()
	if batch.finished {
		batch.mu.Unlock()
		return
	}
	limit := batch.maxWorkers
	if batch.allowLateWorkers {
		if available := c.parallelAvailableConnections(); available > limit {
			// Staged relay transport opens extra connections after the batch
			// starts; let those workers join without waiting for a new batch.
			limit = available
		}
	}
	if limit > 8 {
		limit = 8
	}
	if limit > 0 && len(batch.workers) >= limit {
		batch.mu.Unlock()
		return
	}
	if _, exists := batch.workers[conn]; exists {
		batch.mu.Unlock()
		return
	}
	batch.workers[conn] = struct{}{}
	batch.mu.Unlock()
	batch.wg.Add(1)
	go func() {
		defer batch.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				batch.cancel(fmt.Errorf("parallel send panic: %v", r))
			}
		}()
		batch.runWorker(c, conn)
	}()
}

func (b *batchSendState) runWorker(c *Client, conn *comm.Comm) {
	b.mu.Lock()
	inFlight := b.inFlight
	b.mu.Unlock()
	if inFlight < 1 {
		inFlight = 1
	}
	out := make(chan outFrame, inFlight)
	writerDone := make(chan error, 1)
	go func() {
		for frame := range out {
			if err := conn.Send(frame.wire); err != nil {
				b.cancel(transferDisconnectError{err: err})
				writerDone <- err
				return
			}
			b.sentBytes.Add(frame.n)
			c.addParallelProgress(frame.n)
			b.queue.complete(frame.task)
		}
		writerDone <- nil
	}()
	for {
		task, ok := b.queue.claim(b.ctx)
		if !ok {
			break
		}
		wire, n, err := b.buildFrame(c, task)
		if err != nil {
			if b.ctx.Err() == nil {
				b.cancel(err)
			}
			close(out)
			<-writerDone
			return
		}
		blockedAt := time.Now()
		select {
		case out <- outFrame{wire: wire, n: n, task: task}:
			b.blockedNanos.Add(int64(time.Since(blockedAt)))
		case <-b.ctx.Done():
			b.blockedNanos.Add(int64(time.Since(blockedAt)))
			close(out)
			<-writerDone
			return
		}
	}
	close(out)
	if err := <-writerDone; err != nil {
		if c.ctxErr() == nil {
			b.cancel(transferDisconnectError{err: err})
		}
	}
}

func (b *batchSendState) buildFrame(c *Client, task chunkTask) ([]byte, int64, error) {
	if task.file == nil {
		return nil, 0, fmt.Errorf("send task has no open file")
	}
	task.file.mu.Lock()
	file := task.file.file
	task.file.mu.Unlock()
	if file == nil {
		return nil, 0, fmt.Errorf("send task has no open file")
	}
	payload := make([]byte, task.length)
	n, err := file.ReadAt(payload, task.offset)
	if n != task.length {
		if err == nil || err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, 0, fmt.Errorf("read chunk at %d: %w", task.offset, err)
	}
	flags := byte(0)
	if task.offset+int64(task.length) == task.file.size {
		flags |= parallelFlagFinal
	}
	if task.file.compressed {
		payload = compress.CompressTo(nil, payload)
		flags |= parallelFlagCompressed
	}
	if c.limiter != nil {
		reservation := c.limiter.ReserveN(time.Now(), task.length)
		if delay := reservation.Delay(); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-b.ctx.Done():
				reservation.Cancel()
				timer.Stop()
				return nil, 0, b.ctx.Err()
			}
		}
	}
	plain, err := encodeDataFrameV2(nil, flags, b.id, uint32(task.file.index), task.offset, payload)
	if err != nil {
		return nil, 0, err
	}
	wire, err := crypt.EncryptAEADTo(nil, plain, c.dataAEAD)
	if err != nil {
		return nil, 0, err
	}
	return wire, int64(task.length), nil
}

func (b *batchSendState) cancel(err error) {
	b.mu.Lock()
	if b.finished {
		b.mu.Unlock()
		return
	}
	b.finished = true
	b.mu.Unlock()
	if b.cancelFn != nil {
		b.cancelFn()
	}
	if b.queue != nil {
		b.queue.close()
	}
	if err != nil && b.attempt != nil {
		b.attempt.report(err)
	}
}

func (c *Client) processParallelFileDone(m message.Message) error {
	if !c.Options.IsSender {
		return fmt.Errorf("unexpected parallel file-done")
	}
	batch := c.currentParallelSend()
	if batch == nil {
		return fmt.Errorf("parallel file-done without an active batch")
	}
	var done ParallelFileDone
	if err := json.Unmarshal(m.Bytes, &done); err != nil {
		return fmt.Errorf("decode parallel file-done: %w", err)
	}
	if done.BatchID != batch.id {
		return fmt.Errorf("parallel file-done batch %d does not match %d", done.BatchID, batch.id)
	}
	if done.Index < 0 || done.Index >= len(c.FilesToTransfer) {
		return fmt.Errorf("parallel file-done index %d out of range", done.Index)
	}
	batch.mu.Lock()
	if _, exists := batch.done[uint32(done.Index)]; exists {
		batch.mu.Unlock()
		return nil
	}
	batch.mu.Unlock()
	state := batch.files[uint32(done.Index)]
	if state == nil {
		return fmt.Errorf("parallel file-done for file outside the batch")
	}
	switch done.Status {
	case parallelStatusOK:
		if len(done.Hash) == 0 || !bytes.Equal(done.Hash, c.FilesToTransfer[done.Index].Hash) {
			return fmt.Errorf("parallel file-done hash does not match the manifest")
		}
	case parallelStatusSkipped:
	case parallelStatusHashMismatch:
	case parallelStatusError:
	default:
		return fmt.Errorf("unknown parallel file-done status %q", done.Status)
	}
	switch done.Status {
	case parallelStatusHashMismatch:
		batch.mu.Lock()
		retries := batch.retries[uint32(done.Index)]
		batch.retries[uint32(done.Index)] = retries + 1
		batch.mu.Unlock()
		c.statsAddRetry()
		if retries >= 1 {
			batch.cancel(fmt.Errorf("file %d failed hash verification twice", done.Index))
			return nil
		}
		idleCtx, idleCancel := context.WithTimeout(batch.ctx, 5*time.Second)
		idle := batch.queue.waitIdle(idleCtx)
		idleCancel()
		if !idle {
			batch.cancel(fmt.Errorf("timed out waiting for file %d send workers", done.Index))
			return nil
		}
		if err := state.resetForRetry(); err != nil {
			batch.cancel(err)
			return nil
		}
		batch.queue.addFile(state)
		log.Debugf("parallel: retrying file %d after hash mismatch", done.Index)
		return nil
	case parallelStatusError:
		batch.cancel(fmt.Errorf("receiver reported an error for file %d", done.Index))
		return nil
	}

	batch.mu.Lock()
	if _, exists := batch.done[uint32(done.Index)]; exists {
		batch.mu.Unlock()
		return nil
	}
	batch.done[uint32(done.Index)] = done.Status
	batch.results[uint32(done.Index)] = done
	allDone := len(batch.done) == batch.total
	batch.mu.Unlock()
	if allDone {
		return c.sendParallelBatchDone(batch)
	}
	return nil
}

func (c *Client) sendParallelBatchDone(batch *batchSendState) error {
	batch.mu.Lock()
	if batch.finished {
		batch.mu.Unlock()
		return nil
	}
	batch.finished = true
	results := make([]ParallelFileDone, 0, len(batch.order))
	status := parallelStatusOK
	for _, index := range batch.order {
		done := batch.results[index]
		if done.Status == "" {
			done = ParallelFileDone{BatchID: batch.id, Index: int(index), Status: parallelStatusSkipped}
		}
		if done.Status == parallelStatusError {
			status = parallelStatusError
		}
		results = append(results, done)
	}
	batch.mu.Unlock()
	body, err := json.Marshal(ParallelBatchDone{BatchID: batch.id, Status: status, Files: results})
	if err != nil {
		batch.cancel(err)
		return err
	}
	if err := c.sendParallelMessage(message.Message{Type: message.TypeParallelBatchDone, Bytes: body}); err != nil {
		if batch.cancelFn != nil {
			batch.cancelFn()
		}
		batch.queue.close()
		if batch.attempt != nil {
			batch.attempt.report(transferDisconnectError{err: err})
		}
		return err
	}
	batch.queue.close()
	c.clearParallelSend(batch)
	if batch.cancelFn != nil {
		batch.cancelFn()
	}
	c.updateLifecycle(func(state *transferLifecycle) { state.FileTransferred = false })
	c.finishParallelProgress(len(batch.order))
	go func() {
		finished := make(chan struct{})
		go func() {
			batch.wg.Wait()
			close(finished)
		}()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			log.Debug("parallel: workers did not stop after batch done")
		}
		for _, state := range batch.files {
			state.close()
		}
	}()
	return nil
}

func (b *batchSendState) abortClose() {
	b.cancel(nil)
	for _, state := range b.files {
		state.close()
	}
}
