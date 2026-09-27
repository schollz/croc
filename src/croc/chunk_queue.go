package croc

import (
	"errors"
	"os"
	"sync"

	"github.com/schollz/croc/v11/src/comm"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/utils"
)

// requestedChunkQueue assigns every requested file offset to exactly one
// worker. Workers may join after transfer start without changing ownership of
// chunks already claimed by another connection. Offsets are generated from the
// chunk ranges as they are claimed, so the queue does not grow with the file.
type requestedChunkQueue struct {
	mu        sync.Mutex
	ranges    []int64
	next      int   // index of the current range start in ranges
	step      int64 // chunks already claimed from the current range
	total     int
	completed int
	done      sync.Once
	onDone    func()
}

func newRequestedChunkQueue(ranges []int64, fileSize, chunkSize int64, onDone func()) *requestedChunkQueue {
	if len(ranges) == 0 {
		ranges = []int64{chunkSize, 0, int64(utils.ChunkRangesCount(nil, fileSize, chunkSize))}
	}
	queue := &requestedChunkQueue{ranges: ranges, next: 1, onDone: onDone}
	// claim hands out exactly total offsets, otherwise onDone would never run
	for i := 2; i < len(ranges); i += 2 {
		if ranges[i] > 0 {
			queue.total += int(ranges[i])
		}
	}
	if queue.total == 0 {
		queue.signalDone()
	}
	return queue
}

func (q *requestedChunkQueue) claim() (int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for ; q.next+1 < len(q.ranges); q.next += 2 {
		if q.step < q.ranges[q.next+1] {
			offset := q.ranges[q.next] + q.step*q.ranges[0]
			q.step++
			return offset, true
		}
		q.step = 0
	}
	return 0, false
}

func (q *requestedChunkQueue) complete() {
	q.mu.Lock()
	q.completed++
	finished := q.completed == q.total
	q.mu.Unlock()
	if finished {
		q.signalDone()
	}
}

func (q *requestedChunkQueue) signalDone() {
	q.done.Do(func() {
		if q.onDone != nil {
			q.onDone()
		}
	})
}

func (c *Client) startSenderChunkQueue(attempt *transferAttemptState, file *os.File) {
	queue := newRequestedChunkQueue(
		c.CurrentFileChunkRanges,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		models.TCP_BUFFER_SIZE/2,
		func() {
			log.Debug("closing file")
			if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				log.Errorf("error closing file: %v", err)
			}
		},
	)
	c.senderDataMu.Lock()
	c.senderChunkQueue = queue
	c.senderDataAttempt = attempt
	c.senderDataFile = file
	c.senderDataWorkers = make(map[*comm.Comm]struct{})
	c.senderWorkerSequence = 0
	c.senderDataMu.Unlock()

	connections := c.connectionsSnapshot()
	if len(connections) > 0 {
		connections = connections[1:]
	}
	for i, connection := range connections {
		if connection != nil {
			log.Debugf("starting sending over comm %d", i)
			c.startLateSenderWorker(connection)
		}
	}
}

func (c *Client) startLateSenderWorker(dataConn *comm.Comm) {
	if dataConn == nil {
		return
	}
	c.senderDataMu.Lock()
	queue, file, attempt := c.senderChunkQueue, c.senderDataFile, c.senderDataAttempt
	if queue == nil || file == nil || attempt == nil {
		c.senderDataMu.Unlock()
		return
	}
	if _, exists := c.senderDataWorkers[dataConn]; exists {
		c.senderDataMu.Unlock()
		return
	}
	c.senderDataWorkers[dataConn] = struct{}{}
	workerID := c.senderWorkerSequence
	c.senderWorkerSequence++
	c.senderDataMu.Unlock()
	go c.sendData(workerID, dataConn, file, queue, attempt)
}
