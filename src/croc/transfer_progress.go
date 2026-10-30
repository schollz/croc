package croc

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/progressbar/v3"
)

// A negotiated bundle session shares one visible bar across bundles and the
// individual files between them. Per-file bars still drive completion callbacks.
type transferProgressDisplay struct {
	mu           sync.Mutex
	bar          *progressbar.ProgressBar
	output       io.Writer
	colorEnabled bool
	totalFiles   int
	fileNumber   int
	filename     string
	unitSize     int64
	unitDone     int64
	closed       bool
}

func newTransferProgressDisplay(files []FileInfo, folders int, output io.Writer, colorEnabled bool) *transferProgressDisplay {
	total := int64(folders)
	for _, file := range files {
		// Empty files use the same one-byte indicator as individual transfers.
		total += max(int64(1), file.Size)
	}
	p := &transferProgressDisplay{output: output, colorEnabled: colorEnabled, totalFiles: len(files) + folders}
	p.bar = termui.NewProgress(termui.ProgressConfig{
		Max:          max(int64(1), total),
		Writer:       output,
		ColorEnabled: colorEnabled,
		Throttle:     100 * time.Millisecond,
	})
	return p
}

func (c *Client) progressDisplaySnapshot() *transferProgressDisplay {
	c.barMu.RLock()
	defer c.barMu.RUnlock()
	return c.progressDisplay
}

func (c *Client) ensureProgressDisplay(bundle bool) *transferProgressDisplay {
	if !bundle && !(c.bundles.peerHandshake && (c.Options.IsSender || c.bundles.manifest) && c.bundleTransferEligible() && len(c.FilesToTransfer) > 1) {
		return nil
	}
	c.barMu.Lock()
	defer c.barMu.Unlock()
	if c.progressDisplay == nil {
		output, colorEnabled := termui.Output(os.Stderr)
		c.progressDisplay = newTransferProgressDisplay(c.FilesToTransfer, len(c.EmptyFoldersToTransfer), output, colorEnabled)
	}
	return c.progressDisplay
}

func (p *transferProgressDisplay) describe(filename string, number int) {
	if filename == "" {
		filename = p.filename
	}
	number = min(p.totalFiles, max(p.fileNumber, number))
	if filename == p.filename && number == p.fileNumber {
		return
	}
	p.filename, p.fileNumber = filename, number
	p.bar.Describe(termui.ProgressDescription(fmt.Sprintf("%d/%d ", p.fileNumber, p.totalFiles), p.filename, p.colorEnabled))
}

func (p *transferProgressDisplay) begin(size int64, filename string, number int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unitSize, p.unitDone = size, 0
	p.describe(filename, number)
}

func (p *transferProgressDisplay) advance(filename string, number int, delta int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if filename != "" && (filename != p.filename || number > p.fileNumber) {
		p.describe(filename, number)
	}
	delta = min(max(int64(0), delta), p.unitSize-p.unitDone)
	p.unitDone += delta
	_ = p.bar.Add64(delta)
}

func (p *transferProgressDisplay) finishUnit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.bar.Add64(p.unitSize - p.unitDone)
	p.unitDone = p.unitSize
}

func (p *transferProgressDisplay) completeFile(number int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.describe("", number)
}

func (p *transferProgressDisplay) close(success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if success {
		p.describe("", p.totalFiles)
		_ = p.bar.Finish()
	}
	_, _ = fmt.Fprintln(p.output)
}
