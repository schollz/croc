package croc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSendFile(index int, size, chunkSize int64) *fileSendState {
	count := parallelChunkCount(size, chunkSize)
	state := &fileSendState{
		index:      index,
		size:       size,
		chunkSize:  chunkSize,
		chunkCount: count,
		ranges:     []int64{chunkSize, 0, count},
		fullRanges: []int64{chunkSize, 0, count},
		cursor:     1,
	}
	state.remaining.Store(count)
	return state
}

func TestParallelQueueRoundRobin(t *testing.T) {
	a := testSendFile(0, 30, 10)
	b := testSendFile(1, 20, 10)
	q := newParallelChunkQueue([]*fileSendState{a, b})
	ctx := context.Background()

	var order []string
	for {
		task, ok := q.claim(ctx)
		if !ok {
			break
		}
		order = append(order, string(rune('a'+task.file.index))+":"+itoa(task.offset))
		q.complete(task)
		if len(order) == 5 {
			q.close()
		}
	}
	assert.Equal(t, []string{"a:0", "b:0", "a:10", "b:10", "a:20"}, order)
	assert.Equal(t, int64(0), a.remaining.Load())
	assert.Equal(t, int64(0), b.remaining.Load())
}

func TestParallelQueueWaitsForRetry(t *testing.T) {
	a := testSendFile(0, 20, 10)
	q := newParallelChunkQueue([]*fileSendState{a})
	task, ok := q.claim(context.Background())
	require.True(t, ok)
	q.complete(task)
	task, ok = q.claim(context.Background())
	require.True(t, ok)
	q.complete(task)

	got := make(chan chunkTask, 1)
	go func() {
		task, ok := q.claim(context.Background())
		if ok {
			got <- task
		}
	}()
	select {
	case <-got:
		t.Fatal("claim returned while the queue was empty")
	case <-time.After(50 * time.Millisecond):
	}
	a.ranges = []int64{10, 0, 2}
	a.cursor = 1
	a.step = 0
	a.remaining.Store(2)
	q.addFile(a)
	select {
	case task := <-got:
		assert.Equal(t, int64(0), task.offset)
	case <-time.After(time.Second):
		t.Fatal("claim did not wake after addFile")
	}
	q.close()
}

func TestParallelQueueCloseDrains(t *testing.T) {
	a := testSendFile(0, 20, 10)
	q := newParallelChunkQueue([]*fileSendState{a})
	q.close()
	task, ok := q.claim(context.Background())
	require.True(t, ok)
	assert.Equal(t, int64(0), task.offset)
	q.complete(task)
	task, ok = q.claim(context.Background())
	require.True(t, ok)
	q.complete(task)
	_, ok = q.claim(context.Background())
	assert.False(t, ok)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}
