package uiprogress

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoppingPrintout(t *testing.T) {
	progress := New()
	progress.SetRefreshInterval(time.Millisecond * 10)

	var buffer = &bytes.Buffer{}
	progress.SetOut(buffer)

	bar := progress.AddBar(100)
	progress.Start()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		for i := 0; i <= 80; i = i + 10 {
			bar.Set(i)
			time.Sleep(time.Millisecond * 5)
		}

		wg.Done()
	}()

	wg.Wait()

	progress.Stop()
	fmt.Fprintf(buffer, "foo")

	var wantSuffix = "[======================================================>-------------]\nfoo"

	if !strings.HasSuffix(buffer.String(), wantSuffix) {
		t.Errorf("Content that should be printed after stop not appearing on buffer.")
	}
}
