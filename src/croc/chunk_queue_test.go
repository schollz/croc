package croc

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/utils"
)

func TestRequestedChunkQueueLateWorkersClaimExactlyOnce(t *testing.T) {
	const (
		chunkSize = int64(32)
		fileSize  = int64(32 * 101)
	)
	var done atomic.Int32
	queue := newRequestedChunkQueue(nil, fileSize, chunkSize, func() { done.Add(1) })
	seen := make(map[int64]int)
	var seenMu sync.Mutex
	worker := func() {
		for {
			offset, ok := queue.claim()
			if !ok {
				return
			}
			seenMu.Lock()
			seen[offset]++
			seenMu.Unlock()
			queue.complete()
		}
	}

	var workers sync.WaitGroup
	workers.Go(func() { ; worker() })
	for range 7 {
		workers.Go(func() { ; worker() })
	}
	workers.Wait()
	if len(seen) != 101 {
		t.Fatalf("claimed %d chunks", len(seen))
	}
	for offset, count := range seen {
		if count != 1 {
			t.Fatalf("offset %d claimed %d times", offset, count)
		}
	}
	if done.Load() != 1 {
		t.Fatalf("completion callback ran %d times", done.Load())
	}
}

func TestRequestedChunkQueueHonorsResumeRanges(t *testing.T) {
	queue := newRequestedChunkQueue([]int64{16, 32, 2, 80, 1}, 128, 16, nil)
	var got []int64
	for {
		offset, ok := queue.claim()
		if !ok {
			break
		}
		got = append(got, offset)
		queue.complete()
	}
	want := []int64{32, 48, 80}
	if len(got) != len(want) {
		t.Fatalf("offsets = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("offsets = %v, want %v", got, want)
		}
	}
}

func TestRequestedChunkQueueCompletesEmptyRequest(t *testing.T) {
	var done atomic.Int32
	queue := newRequestedChunkQueue(nil, 0, 16, func() { done.Add(1) })
	if _, ok := queue.claim(); ok || done.Load() != 1 {
		t.Fatalf("empty queue claim=%v completion calls=%d", ok, done.Load())
	}
}

func TestRequestedChunkQueueMatchesChunkRangesToChunks(t *testing.T) {
	for _, ranges := range [][]int64{
		{16},
		{16, 48, 4},
		{16, 32, 2, 80, 1},
		{16, 0, 0, 32, 2},
		{16, 0, -3, 32, 2},
		{16, 80, 1, 0, 2},
	} {
		var done atomic.Int32
		queue := newRequestedChunkQueue(ranges, 128, 16, func() { done.Add(1) })
		var got []int64
		for {
			offset, ok := queue.claim()
			if !ok {
				break
			}
			got = append(got, offset)
			queue.complete()
		}
		if want := utils.ChunkRangesToChunks(ranges); !slices.Equal(got, want) {
			t.Fatalf("ranges %v: offsets = %v, want %v", ranges, got, want)
		}
		if done.Load() != 1 {
			t.Fatalf("ranges %v: completion callback ran %d times", ranges, done.Load())
		}
	}
}

func TestRequestedChunkQueueLargeFileDoesNotListOffsets(t *testing.T) {
	const (
		chunkSize = int64(models.TCP_BUFFER_SIZE / 2)
		fileSize  = int64(100 << 30)
	)
	var done atomic.Int32
	queue := newRequestedChunkQueue(nil, fileSize, chunkSize, func() { done.Add(1) })
	if len(queue.ranges) != 3 {
		t.Fatalf("queue for a 100 GiB file keeps %d range entries", len(queue.ranges))
	}
	var claimed int64
	for {
		offset, ok := queue.claim()
		if !ok {
			break
		}
		if offset != claimed*chunkSize {
			t.Fatalf("claim %d returned offset %d", claimed, offset)
		}
		claimed++
		queue.complete()
	}
	if claimed != fileSize/chunkSize || done.Load() != 1 {
		t.Fatalf("claimed %d chunks, completion callback ran %d times", claimed, done.Load())
	}
}

func BenchmarkRequestedChunkQueueNew(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int64
	}{
		{"1GiB", 1 << 30},
		{"100GiB", 100 << 30},
	} {
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				newRequestedChunkQueue(nil, size.bytes, models.TCP_BUFFER_SIZE/2, nil)
			}
		})
	}
}
