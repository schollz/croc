package croc

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/progressbar/v3"
)

type parallelProgress struct {
	totalFiles int
	totalBytes int64
	doneBytes  atomic.Int64
	started    time.Time
}

func (c *Client) newParallelBar(totalBytes int64, files int) *progressbar.ProgressBar {
	output, colorEnabled := termui.Output(os.Stderr)
	description := fmt.Sprintf("%d files", files)
	if files == 1 {
		description = "file transfer"
	}
	return termui.NewProgress(termui.ProgressConfig{
		Max:          totalBytes,
		Description:  termui.ProgressDescription("", description, colorEnabled),
		Writer:       output,
		ColorEnabled: colorEnabled,
		Hidden:       c.Options.SendingText,
		Throttle:     100 * time.Millisecond,
	})
}

func (c *Client) startParallelProgress(totalBytes int64, files int) *parallelProgress {
	p := &parallelProgress{totalFiles: files, totalBytes: totalBytes, started: time.Now()}
	if totalBytes <= 0 {
		totalBytes = 1
	}
	c.setProgressBar(c.newParallelBar(totalBytes, files))
	c.statsStartTransfer(totalBytes, files)
	return p
}

func (c *Client) addParallelProgress(n int64) {
	if n > 0 {
		c.addProgress(n)
		c.statsAddPayload(n)
	}
}

func (c *Client) finishParallelProgress(files int) {
	c.finishProgress()
	c.progressMu.Lock()
	c.finishedNum += files
	finished := c.finishedNum
	total := c.TotalNumberOfContents
	c.progressMu.Unlock()
	if total > 1 {
		output, colorEnabled := termui.Output(os.Stderr)
		fmt.Fprintln(output, termui.Success(fmt.Sprintf(" %d/%d", finished, total), colorEnabled))
	} else {
		fmt.Fprintf(os.Stderr, "\n")
	}
}

// transferStats is a small, local-only summary emitted by --stats.
type transferStats struct {
	mu        sync.Mutex
	started   time.Time
	elapsed   time.Duration
	payload   atomic.Int64
	files     atomic.Int64
	retries   atomic.Int64
	resumed   atomic.Int64
	peakConns atomic.Int64
	rtt       time.Duration
	chunk     int64
	workers   int
}

func (c *Client) statsStartTransfer(totalBytes int64, files int) {
	c.stats.mu.Lock()
	if c.stats.started.IsZero() {
		c.stats.started = time.Now()
	}
	c.stats.mu.Unlock()
	c.stats.files.Add(int64(files))
}

func (c *Client) statsAddPayload(n int64) {
	if n > 0 {
		c.stats.payload.Add(n)
	}
}

func (c *Client) statsSetLimits(rtt time.Duration, chunk int64, workers int) {
	c.stats.mu.Lock()
	defer c.stats.mu.Unlock()
	if rtt > 0 {
		c.stats.rtt = rtt
	}
	if chunk > 0 {
		c.stats.chunk = chunk
	}
	if workers > 0 {
		c.stats.workers = workers
	}
}

func (c *Client) statsAddRetry() { c.stats.retries.Add(1) }
func (c *Client) statsAddResumed(n int64) {
	if n > 0 {
		c.stats.resumed.Add(n)
	}
}
func (c *Client) statsPeakConnections(n int) {
	for {
		current := c.stats.peakConns.Load()
		if int64(n) <= current || c.stats.peakConns.CompareAndSwap(current, int64(n)) {
			return
		}
	}
}

func (c *Client) printTransferStats() {
	if !c.Options.Stats {
		return
	}
	c.stats.mu.Lock()
	start := c.stats.started
	rtt := c.stats.rtt
	chunk := c.stats.chunk
	workers := c.stats.workers
	c.stats.mu.Unlock()
	elapsed := time.Duration(0)
	if !start.IsZero() {
		elapsed = time.Since(start)
	}
	output, colorEnabled := termui.Output(os.Stderr)
	stats := fmt.Sprintf("stats: bytes=%d files=%d retries=%d resumed=%d elapsed=%s",
		c.stats.payload.Load(),
		c.stats.files.Load(),
		c.stats.retries.Load(),
		c.stats.resumed.Load(),
		elapsed.Round(time.Millisecond),
	)
	if chunk > 0 {
		stats += fmt.Sprintf(" rtt=%s chunk=%d workers=%d", rtt.Round(time.Microsecond), chunk, workers)
	}
	fmt.Fprintln(output, termui.Emphasis(stats, colorEnabled))
}
