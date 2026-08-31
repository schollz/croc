package croc

import (
	"sync"
	"sync/atomic"
	"testing"
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
