package croc

import (
	"context"
	"sync"
)

// chunkTask is one chunk claimed from the fair queue.
type chunkTask struct {
	file   *fileSendState
	offset int64
	length int
}

// parallelChunkQueue hands chunks from several files to workers in round-robin
// order. Claimed chunks are never handed out twice. The queue does not close
// itself when empty: workers stay alive so a file can be re-queued after a
// receiver-side hash mismatch.
type parallelChunkQueue struct {
	mu       sync.Mutex
	files    []*fileSendState
	next     int
	pending  int64
	inflight int
	closed   bool
	wait     chan struct{}
}

func newParallelChunkQueue(files []*fileSendState) *parallelChunkQueue {
	q := &parallelChunkQueue{files: files, wait: make(chan struct{})}
	for _, f := range files {
		q.pending += f.remaining.Load()
	}
	return q
}

func (q *parallelChunkQueue) wakeLocked() {
	close(q.wait)
	q.wait = make(chan struct{})
}

func (q *parallelChunkQueue) claim(ctx context.Context) (chunkTask, bool) {
	for {
		q.mu.Lock()
		if q.closed && q.pending == 0 {
			q.mu.Unlock()
			return chunkTask{}, false
		}
		if q.pending == 0 {
			wait := q.wait
			q.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return chunkTask{}, false
			}
		}
		for i := 0; i < len(q.files); i++ {
			index := (q.next + i) % len(q.files)
			file := q.files[index]
			offset, length, ok := file.claimChunk()
			if !ok {
				continue
			}
			q.next = (index + 1) % len(q.files)
			q.pending--
			q.inflight++
			q.mu.Unlock()
			return chunkTask{file: file, offset: offset, length: length}, true
		}
		// A bookkeeping mismatch must not spin forever. Recompute pending from
		// the file cursors so a retry can still make progress.
		q.pending = 0
		for _, f := range q.files {
			q.pending += f.remaining.Load()
		}
		q.mu.Unlock()
	}
}

func (q *parallelChunkQueue) complete(task chunkTask) {
	q.mu.Lock()
	if q.inflight > 0 {
		q.inflight--
	}
	if q.inflight == 0 {
		q.wakeLocked()
	}
	q.mu.Unlock()
	if task.file != nil {
		task.file.completeChunk()
	}
}

// waitIdle waits until every claimed chunk has been completed. It is used
// before resetting a file for retry so a late completion cannot corrupt the
// new cursor state.
func (q *parallelChunkQueue) waitIdle(ctx context.Context) bool {
	for {
		q.mu.Lock()
		if q.pending == 0 && q.inflight == 0 {
			q.mu.Unlock()
			return true
		}
		wait := q.wait
		q.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return false
		}
	}
}

func (q *parallelChunkQueue) addFile(file *fileSendState) {
	q.mu.Lock()
	q.pending += file.remaining.Load()
	q.wakeLocked()
	q.mu.Unlock()
}

func (q *parallelChunkQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.wakeLocked()
	q.mu.Unlock()
}

func (q *parallelChunkQueue) pendingChunks() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pending
}

func defaultParallelChunkSize(fileSize int64) int64 {
	chunk := parallelDefaultChunkSize
	if fileSize > 0 && fileSize < chunk {
		chunk = fileSize
	}
	if chunk <= 0 {
		return 32 << 10
	}
	if chunk > parallelMaxChunkSize {
		chunk = parallelMaxChunkSize
	}
	return chunk
}
