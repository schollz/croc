package croc

import (
	"context"
	"fmt"
	"math/bits"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/transferperf"
)

// parallelFeatureList returns the local capability set in a stable order.
func (c *Client) parallelFeatureList() []string {
	if c.Options.ParallelData == "off" {
		return nil
	}
	return []string{
		parallelDataFeature,
		chunkResumeFeature,
		parallelSmallFilesFeature,
		transferAutotuneFeature,
	}
}

func (c *Client) setPeerParallelFeatures(features []string) {
	c.peerParallelData = supportsFeature(features, parallelDataFeature)
	c.peerChunkResume = supportsFeature(features, chunkResumeFeature)
	c.peerSmallFiles = supportsFeature(features, parallelSmallFilesFeature)
	c.peerAutotune = supportsFeature(features, transferAutotuneFeature)
}

// parallelEnabled reports whether both peers can use the v2 data plane for
// this transfer. Output-sensitive modes stay on the legacy path.
func (c *Client) parallelEnabled() bool {
	if c.Options.ParallelData == "off" || !c.peerParallelData {
		return false
	}
	if c.Options.Stdout || c.Options.SendingText {
		return false
	}
	return true
}

func (c *Client) parallelResumeEnabled() bool {
	return c.parallelEnabled() && c.peerChunkResume && !c.Options.NoResume
}

func (c *Client) parallelNoMultiplexing() bool {
	return c.Options.NoMultiplexing && c.selectedDataTransport.Load() != selectedTransportTailcat
}

func (c *Client) parallelSmallFilesEnabled() bool {
	return c.parallelEnabled() && c.peerSmallFiles && !c.parallelNoMultiplexing()
}

func (c *Client) parallelMaxFiles() int {
	if !c.parallelSmallFilesEnabled() {
		return 1
	}
	if c.Options.ParallelFiles > 0 {
		return min(c.Options.ParallelFiles, parallelMaxBatchFiles)
	}
	workers := runtime.NumCPU() / 2
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	return min(workers, parallelMaxBatchFiles)
}

func (c *Client) parallelAvailableConnections() int {
	connections := c.connectionsSnapshot()
	available := len(connections) - 1
	if available < 1 {
		available = 1
	}
	if c.parallelNoMultiplexing() {
		return 1
	}
	return available
}

func (c *Client) effectiveMaxChunkSize() int64 {
	limit := parallelMaxChunkSize
	if c.Options.ChunkSize > 0 && c.Options.ChunkSize < limit {
		limit = c.Options.ChunkSize
	}
	if limit > 0 && limit < parallelMinChunkSize {
		limit = parallelMinChunkSize
	}
	return limit
}

func (c *Client) ensurePerfController(available int) *transferperf.Adaptive {
	c.perfMu.Lock()
	defer c.perfMu.Unlock()
	if c.perfController != nil {
		return c.perfController
	}
	profile := transferperf.SystemProfile{CPUs: runtime.NumCPU(), Platform: runtime.GOOS}
	if !c.Options.NoAutotune {
		probe := &transferperf.DefaultProbe{CacheDir: os.TempDir()}
		ctx, cancel := context.WithTimeout(c.clientContext(), 300*time.Millisecond)
		profile = probe.Probe(ctx)
		cancel()
	}
	c.perfProfile = profile
	options := transferperf.Options{
		MaxConnections: c.Options.ParallelConnections,
		MaxChunkSize:   c.effectiveMaxChunkSize(),
		MaxFiles:       c.parallelMaxFiles(),
		MaxInFlight:    4,
		Fixed:          c.Options.NoAutotune,
	}
	c.perfController = transferperf.NewController(profile, available, options)
	c.perfController.Initial(profile, available)
	return c.perfController
}

func (c *Client) parallelLimits(available int) transferperf.Limits {
	return c.ensurePerfController(available).Current()
}

func (c *Client) parallelChunkSizeFor(fileSize int64) int64 {
	limits := c.parallelLimits(c.parallelAvailableConnections())
	chunk := limits.ChunkSize
	if c.Options.ChunkSize > 0 && c.Options.ChunkSize < chunk {
		chunk = c.Options.ChunkSize
	}
	if chunk > parallelMaxChunkSize {
		chunk = parallelMaxChunkSize
	}
	if fileSize > 0 && chunk > fileSize {
		chunk = fileSize
	}
	if chunk < 1 {
		chunk = defaultParallelChunkSize(fileSize)
	}
	return chunk
}

func (c *Client) perfProfileSnapshot() transferperf.SystemProfile {
	c.perfMu.Lock()
	defer c.perfMu.Unlock()
	return c.perfProfile
}

func (c *Client) sendParallelMessage(m message.Message) error {
	c.controlSendMu.Lock()
	defer c.controlSendMu.Unlock()
	return message.Send(c.connection(0), c.Key, m)
}

func (c *Client) currentParallelSend() *batchSendState {
	c.parallelMu.Lock()
	defer c.parallelMu.Unlock()
	return c.parallelSend
}

func (c *Client) currentParallelRecv() *batchReceiveState {
	c.parallelMu.Lock()
	defer c.parallelMu.Unlock()
	return c.parallelRecv
}

func (c *Client) setParallelSend(state *batchSendState) {
	c.parallelMu.Lock()
	c.parallelSend = state
	c.parallelMu.Unlock()
}

func (c *Client) clearParallelSend(state *batchSendState) {
	c.parallelMu.Lock()
	if c.parallelSend == state {
		c.parallelSend = nil
	}
	c.parallelMu.Unlock()
}

func (c *Client) setParallelRecv(state *batchReceiveState) {
	c.parallelMu.Lock()
	c.parallelRecv = state
	c.parallelMu.Unlock()
}

func (c *Client) clearParallelRecv(state *batchReceiveState) {
	c.parallelMu.Lock()
	if c.parallelRecv == state {
		c.parallelRecv = nil
	}
	c.parallelMu.Unlock()
}

// stopParallelTransfer cancels any active v2 batch, persists receiver state,
// and closes files. It is called from closeAttempt and shutdown paths.
func (c *Client) stopParallelTransfer() {
	c.parallelMu.Lock()
	send := c.parallelSend
	recv := c.parallelRecv
	c.parallelMu.Unlock()
	if send != nil {
		send.abortClose()
	}
	if recv != nil {
		recv.abort(nil)
	}
}

func (c *Client) resetParallelState() {
	c.parallelMu.Lock()
	c.parallelSend = nil
	c.parallelRecv = nil
	c.parallelLastBatchID = 0
	c.parallelMu.Unlock()
	c.peerParallelData = false
	c.peerChunkResume = false
	c.peerSmallFiles = false
	c.peerAutotune = false
	c.perfMu.Lock()
	c.perfController = nil
	c.perfMu.Unlock()
}

func (c *Client) nextParallelBatchID() uint64 {
	c.parallelMu.Lock()
	defer c.parallelMu.Unlock()
	c.parallelNextBatchID++
	if c.parallelNextBatchID == 0 {
		c.parallelNextBatchID = 1
	}
	return c.parallelNextBatchID
}

func (c *Client) normalizeParallelOptions() error {
	if c.Options.ParallelData == "" {
		c.Options.ParallelData = "auto"
	}
	switch strings.ToLower(c.Options.ParallelData) {
	case "auto", "off", "force":
		c.Options.ParallelData = strings.ToLower(c.Options.ParallelData)
	default:
		return fmt.Errorf("invalid --parallel-data %q (choose auto, off, or force)", c.Options.ParallelData)
	}
	if c.Options.ParallelConnections < 0 || c.Options.ParallelConnections > 8 {
		return fmt.Errorf("--parallel-connections must be between 0 and 8")
	}
	if c.Options.ParallelFiles < 0 || c.Options.ParallelFiles > 16 {
		return fmt.Errorf("--parallel-files must be between 0 and 16")
	}
	if c.Options.ChunkSize != 0 {
		if c.Options.ChunkSize < parallelMinChunkSize || c.Options.ChunkSize > parallelMaxChunkSize {
			return fmt.Errorf("--chunk-size must be between %d and %d bytes", parallelMinChunkSize, parallelMaxChunkSize)
		}
		if bits.OnesCount64(uint64(c.Options.ChunkSize)) != 1 {
			return fmt.Errorf("--chunk-size must be a power of two")
		}
	}
	switch c.Options.ResumeVerify {
	case "":
		c.Options.ResumeVerify = "full"
	case "full", "sample", "none":
	default:
		return fmt.Errorf("invalid --resume-verify %q (choose full, sample, or none)", c.Options.ResumeVerify)
	}
	return nil
}
