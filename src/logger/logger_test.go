package logger

import (
	"io"
	"sync"
	"testing"
)

func TestConcurrentConfigurationAndLogging(t *testing.T) {
	previous := GetLevel()
	t.Cleanup(func() { SetLevel(previous) })
	SetOutput(io.Discard)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			levels := []string{"trace", "debug", "info", "warn", "error"}
			for j := range 100 {
				SetLevel(levels[(i+j)%len(levels)])
				Debugf("worker %d iteration %d", i, j)
			}
		}(i)
	}
	wg.Wait()
}
