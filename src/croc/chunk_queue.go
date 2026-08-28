package croc

import (
	"sync"

	"github.com/schollz/croc/v11/src/utils"
)

// requestedChunkQueue assigns every requested file offset to exactly one
// worker. Workers may join after transfer start without changing ownership of
// chunks already claimed by another connection.
type requestedChunkQueue struct {
	mu        sync.Mutex
	offsets   []int64
	next      int
	completed int
	done      sync.Once
	onDone    func()
}

func newRequestedChunkQueue(ranges []int64, fileSize, chunkSize int64, onDone func()) *requestedChunkQueue {
	var offsets []int64
	if len(ranges) == 0 {
		count := utils.ChunkRangesCount(nil, fileSize, chunkSize)
		offsets = make([]int64, count)
		for i := range offsets {
			offsets[i] = int64(i) * chunkSize
		}
	} else {
		offsets = utils.ChunkRangesToChunks(ranges)
	}
	return &requestedChunkQueue{offsets: offsets, onDone: onDone}
}

func (q *requestedChunkQueue) claim() (int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.next >= len(q.offsets) {
		return 0, false
	}
	offset := q.offsets[q.next]
	q.next++
	return offset, true
}

func (q *requestedChunkQueue) complete() {
	q.mu.Lock()
	q.completed++
	finished := q.completed == len(q.offsets)
	q.mu.Unlock()
	if finished {
		q.done.Do(func() {
			if q.onDone != nil {
				q.onDone()
			}
		})
	}
}

func (q *requestedChunkQueue) counts() (claimed, completed, total int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.next, q.completed, len(q.offsets)
}
