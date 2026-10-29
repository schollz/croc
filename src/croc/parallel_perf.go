package croc

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/transferperf"
)

const (
	parallelPerfSampleInterval = 500 * time.Millisecond
	parallelPerfProbeEvery     = 2 // ticks; one probe per second
	parallelPerfProbeTimeout   = 5 * time.Second
	parallelPerfMaxPending     = 8
)

// ParallelPerfProbe is a control-channel RTT probe. The receiver echoes the ID
// unchanged; the sender measures elapsed time locally.
type ParallelPerfProbe struct {
	ID uint64 `json:"i"`
}

type perfProbeState struct {
	mu       sync.Mutex
	seq      uint64
	probes   map[uint64]time.Time
	rttNanos atomic.Int64
}

func (p *perfProbeState) init() {
	p.mu.Lock()
	p.probes = make(map[uint64]time.Time)
	p.mu.Unlock()
}

func (p *perfProbeState) send(c *Client) {
	if c.Options.NoAutotune || !c.peerAutotune || !c.parallelEnabled() {
		return
	}
	now := time.Now()
	p.mu.Lock()
	if p.probes == nil {
		p.probes = make(map[uint64]time.Time)
	}
	for id, sent := range p.probes {
		if now.Sub(sent) > parallelPerfProbeTimeout {
			delete(p.probes, id)
		}
	}
	if len(p.probes) >= parallelPerfMaxPending {
		p.mu.Unlock()
		return
	}
	id := atomic.AddUint64(&p.seq, 1)
	p.probes[id] = now
	p.mu.Unlock()

	body, err := json.Marshal(ParallelPerfProbe{ID: id})
	if err != nil {
		return
	}
	if err := c.sendParallelMessage(message.Message{Type: message.TypePerfProbe, Bytes: body}); err != nil {
		p.mu.Lock()
		delete(p.probes, id)
		p.mu.Unlock()
	}
}

func (p *perfProbeState) noteReply(id uint64) {
	p.mu.Lock()
	sent, ok := p.probes[id]
	delete(p.probes, id)
	p.mu.Unlock()
	if !ok || sent.IsZero() {
		return
	}
	rtt := time.Since(sent)
	if rtt <= 0 || rtt > 10*time.Second {
		return
	}
	p.rttNanos.Store(int64(rtt))
}

func (p *perfProbeState) rtt() time.Duration {
	return time.Duration(p.rttNanos.Load())
}

func (c *Client) processPerfProbe(m message.Message) error {
	if !c.parallelEnabled() || !c.peerAutotune || c.Options.NoAutotune {
		return nil
	}
	if len(m.Bytes) > 1024 {
		return fmt.Errorf("perf probe is too large")
	}
	var probe ParallelPerfProbe
	if err := json.Unmarshal(m.Bytes, &probe); err != nil {
		return fmt.Errorf("decode perf probe: %w", err)
	}
	if probe.ID == 0 {
		return fmt.Errorf("invalid perf probe id")
	}
	body, err := json.Marshal(probe)
	if err != nil {
		return err
	}
	return c.sendParallelMessage(message.Message{Type: message.TypePerfProbeReply, Bytes: body})
}

func (c *Client) processPerfProbeReply(m message.Message) error {
	if len(m.Bytes) > 1024 {
		return fmt.Errorf("perf probe reply is too large")
	}
	var probe ParallelPerfProbe
	if err := json.Unmarshal(m.Bytes, &probe); err != nil {
		return fmt.Errorf("decode perf probe reply: %w", err)
	}
	if probe.ID == 0 {
		return fmt.Errorf("invalid perf probe reply id")
	}
	if batch := c.currentParallelSend(); batch != nil {
		batch.perf.noteReply(probe.ID)
		c.statsSetLimits(batch.perf.rtt(), 0, 0)
	}
	if batch := c.currentParallelRecv(); batch != nil {
		batch.perf.noteReply(probe.ID)
		c.statsSetLimits(batch.perf.rtt(), 0, 0)
	}
	return nil
}

func (c *Client) runParallelSendSampler(batch *batchSendState) {
	if c.Options.NoAutotune || !c.peerAutotune {
		return
	}
	sampler := transferperf.NewProcessSampler(c.perfProfileSnapshot())
	ticker := time.NewTicker(parallelPerfSampleInterval)
	defer ticker.Stop()
	lastAt := time.Now()
	var lastBytes, lastBlocked int64
	ticks := 0
	for {
		select {
		case <-batch.ctx.Done():
			return
		case now := <-ticker.C:
			ticks++
			bytes := batch.sentBytes.Load()
			blocked := batch.blockedNanos.Load()
			interval := now.Sub(lastAt)
			if interval <= 0 {
				continue
			}
			deltaBytes := bytes - lastBytes
			deltaBlocked := blocked - lastBlocked
			lastBytes, lastBlocked, lastAt = bytes, blocked, now
			if ticks%parallelPerfProbeEvery == 0 {
				batch.perf.send(c)
			}
			cpu, memory := sampler.Sample(now)
			controller := c.ensurePerfController(c.parallelAvailableConnections())
			limits := controller.Observe(transferperf.Sample{
				At:             now,
				Bytes:          deltaBytes,
				Interval:       interval,
				RTT:            batch.perf.rtt(),
				SendBlocked:    time.Duration(deltaBlocked),
				CPUUtilization: cpu,
				MemUtilization: memory,
				QueueDepth:     int(batch.queue.pendingChunks()),
			})
			c.applyParallelSendLimits(batch, limits)
			if deltaBytes > 0 {
				bw := float64(deltaBytes) / interval.Seconds()
				log.Debugf("perf: send rtt=%s bw=%.0fB/s conns=%d chunk=%d inflight=%d",
					batch.perf.rtt(), bw, limits.Connections, limits.ChunkSize, limits.InFlight)
			}
		}
	}
}

func (c *Client) runParallelReceiveSampler(batch *batchReceiveState) {
	if c.Options.NoAutotune || !c.peerAutotune {
		return
	}
	sampler := transferperf.NewProcessSampler(c.perfProfileSnapshot())
	ticker := time.NewTicker(parallelPerfSampleInterval)
	defer ticker.Stop()
	lastAt := time.Now()
	lastBytes := batch.receivedBytes.Load()
	ticks := 0
	for {
		select {
		case <-batch.ctx.Done():
			return
		case now := <-ticker.C:
			ticks++
			bytes := batch.receivedBytes.Load()
			interval := now.Sub(lastAt)
			if interval <= 0 {
				continue
			}
			deltaBytes := bytes - lastBytes
			lastBytes, lastAt = bytes, now
			if ticks%parallelPerfProbeEvery == 0 {
				batch.perf.send(c)
			}
			cpu, memory := sampler.Sample(now)
			controller := c.ensurePerfController(c.parallelAvailableConnections())
			limits := controller.Observe(transferperf.Sample{
				At:             now,
				Bytes:          deltaBytes,
				Interval:       interval,
				RTT:            batch.perf.rtt(),
				CPUUtilization: cpu,
				MemUtilization: memory,
			})
			c.statsSetLimits(batch.perf.rtt(), limits.ChunkSize, limits.Connections)
			if deltaBytes > 0 {
				bw := float64(deltaBytes) / interval.Seconds()
				log.Debugf("perf: recv rtt=%s bw=%.0fB/s chunk=%d files=%d",
					batch.perf.rtt(), bw, limits.ChunkSize, limits.ParallelFiles)
			}
		}
	}
}

func (c *Client) applyParallelSendLimits(batch *batchSendState, limits transferperf.Limits) {
	connections := limits.Connections
	if connections < 1 {
		connections = 1
	}
	if connections > 8 {
		connections = 8
	}
	inFlight := limits.InFlight
	if inFlight < 1 {
		inFlight = 1
	}
	if inFlight > 4 {
		inFlight = 4
	}
	batch.mu.Lock()
	if batch.finished {
		batch.mu.Unlock()
		return
	}
	previous := batch.maxWorkers
	batch.maxWorkers = connections
	batch.inFlight = inFlight
	batch.mu.Unlock()
	c.statsSetLimits(batch.perf.rtt(), limits.ChunkSize, connections)
	if connections > previous {
		for i, conn := range c.connectionsSnapshot() {
			if i == 0 || conn == nil {
				continue
			}
			c.startParallelWorker(conn)
		}
	}
}
