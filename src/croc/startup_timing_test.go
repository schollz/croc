package croc

import (
	"sync"
	"testing"
	"time"
)

func TestStartupTimingRecordsMilestonesOnce(t *testing.T) {
	var timing startupTiming
	timing.start()
	timing.mark("relay-control-ready")
	first, ok := timing.elapsed("relay-control-ready")
	if !ok {
		t.Fatal("relay milestone was not recorded")
	}
	time.Sleep(time.Millisecond)
	timing.mark("relay-control-ready")
	second, ok := timing.elapsed("relay-control-ready")
	if !ok || second != first {
		t.Fatalf("duplicate milestone changed from %v to %v", first, second)
	}
}

func TestStartupTimingConcurrentMarks(t *testing.T) {
	var timing startupTiming
	timing.start()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timing.mark("transport-ready")
		}()
	}
	wg.Wait()
	if _, ok := timing.elapsed("transport-ready"); !ok {
		t.Fatal("concurrent milestone was not recorded")
	}
}
