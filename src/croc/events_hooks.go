package croc

import (
	"time"

	"github.com/schollz/croc/v11/src/events"
)

// progressEventInterval is the minimum time between progress events.
const progressEventInterval = 100 * time.Millisecond

// emitPhase forwards a phase transition to the JSON event stream.
func (c *Client) emitPhase(phase, message string) {
	events.Phase(phase, message)
}

// emitProgress forwards per-file transfer progress to the JSON event
// stream, throttled to progressEventInterval. It is a no-op when events
// are disabled.
func (c *Client) emitProgress(added int64) {
	if !events.Enabled() {
		return
	}
	c.progMu.Lock()
	defer c.progMu.Unlock()

	if c.FilesToTransferCurrentNum != c.progFileNum || !c.progEmitStarted {
		c.progFileNum = c.FilesToTransferCurrentNum
		c.progBytes = 0
		c.progLastBytes = 0
		c.progLastEmit = time.Time{}
		c.progEmitStarted = true
	}

	c.progBytes += added
	if c.FilesToTransferCurrentNum >= len(c.FilesToTransfer) {
		return
	}
	fileInfo := c.FilesToTransfer[c.FilesToTransferCurrentNum]
	if c.progBytes > fileInfo.Size {
		c.progBytes = fileInfo.Size
	}

	now := time.Now()
	if !c.progLastEmit.IsZero() && now.Sub(c.progLastEmit) < progressEventInterval && c.progBytes < fileInfo.Size {
		return
	}

	var speedBps int64
	if !c.progLastEmit.IsZero() && now.After(c.progLastEmit) {
		elapsed := now.Sub(c.progLastEmit).Seconds()
		if elapsed > 0 {
			speedBps = int64(float64(c.progBytes-c.progLastBytes) / elapsed)
		}
	}

	var percent float64
	if fileInfo.Size > 0 {
		percent = float64(c.progBytes) / float64(fileInfo.Size) * 100
	}

	events.Progress(events.ProgressEvent{
		File:             fileInfo.Name,
		BytesTransferred: c.progBytes,
		BytesTotal:       fileInfo.Size,
		Percent:          percent,
		SpeedBps:         speedBps,
	})
	c.progLastEmit = now
	c.progLastBytes = c.progBytes
}

// completedFiles builds the file list for the completion event.
func (c *Client) completedFiles() []events.CompleteFile {
	files := make([]events.CompleteFile, 0, len(c.FilesToTransfer))
	for _, fileInfo := range c.FilesToTransfer {
		files = append(files, events.CompleteFile{
			Name:  fileInfo.Name,
			Bytes: fileInfo.Size,
		})
	}
	return files
}

// emitComplete marks the transfer as finished in the JSON event stream.
// It is a no-op once the transfer has already completed.
func (c *Client) emitComplete() {
	if c.transferComplete.Swap(true) {
		return
	}
	events.Complete(c.completedFiles())
	events.Phase("complete", "transfer complete")
}
