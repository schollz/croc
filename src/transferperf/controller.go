package transferperf

import (
	"math"
	"sync"
	"time"
)

const (
	minChunkSize  = int64(64 << 10)
	maxChunkSize  = int64(4 << 20)
	minWindow     = int64(256 << 10)
	maxWindow     = int64(64 << 20)
	maxInFlight   = 4
	maxWorkers    = 8
	maxParallel   = 16
	defaultMemory = int64(256 << 20)
)

// Limits are the transfer parameters chosen by a controller.
type Limits struct {
	Connections    int
	ChunkSize      int64
	InFlight       int
	ParallelFiles  int
	ReadAheadBytes int64
	Compression    bool
}

// Sample is one observation of transfer behavior.
type Sample struct {
	At             time.Time
	Bytes          int64
	Interval       time.Duration
	RTT            time.Duration
	Retransmits    uint64
	SendBlocked    time.Duration
	CPUUtilization float64
	MemUtilization float64
	DiskReadBps    int64
	DiskWriteBps   int64
	QueueDepth     int
	MaxQueue       int
	Errors         int
}

// Controller computes transfer limits.
type Controller interface {
	Initial(profile SystemProfile, availableConnections int) Limits
	Observe(Sample) Limits
	Current() Limits
}

// Options bound the controller's decisions.
type Options struct {
	MaxConnections int
	MaxChunkSize   int64
	MaxFiles       int
	MaxInFlight    int
	MemoryBudget   int64
	Fixed          bool
}

// Adaptive implements a conservative AIMD/BDP controller.
type Adaptive struct {
	mu             sync.Mutex
	limits         Limits
	maxConnections int
	maxChunk       int64
	maxFiles       int
	maxInFlight    int
	memoryBudget   int64
	fixed          bool

	bw            float64
	rtt           time.Duration
	baseRTT       time.Duration
	lastRetrans   uint64
	goodSamples   int
	cooldownUntil time.Time
}

// NewController creates an adaptive controller. availableConnections is the
// number of usable data channels (relay ports or Tailcat streams).
func NewController(profile SystemProfile, availableConnections int, opts Options) *Adaptive {
	if availableConnections < 1 {
		availableConnections = 1
	}
	maxConnections := opts.MaxConnections
	if maxConnections <= 0 || maxConnections > availableConnections {
		maxConnections = availableConnections
	}
	if maxConnections > maxWorkers {
		maxConnections = maxWorkers
	}
	maxChunk := opts.MaxChunkSize
	if maxChunk <= 0 {
		maxChunk = maxChunkSize
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 4
	}
	if maxFiles > maxParallel {
		maxFiles = maxParallel
	}
	inFlight := opts.MaxInFlight
	if inFlight <= 0 {
		inFlight = 2
	}
	if inFlight > maxInFlight {
		inFlight = maxInFlight
	}
	budget := opts.MemoryBudget
	if budget <= 0 {
		budget = defaultMemory
		if profile.MemoryLimit > 0 && profile.MemoryLimit/10 < budget {
			budget = profile.MemoryLimit / 10
		}
	}
	if budget < 32<<20 {
		budget = 32 << 20
	}
	return &Adaptive{
		limits: Limits{
			Connections:    min(2, maxConnections),
			ChunkSize:      min(int64(256<<10), maxChunk),
			InFlight:       inFlight,
			ParallelFiles:  min(4, maxFiles),
			ReadAheadBytes: 0,
			Compression:    true,
		},
		maxConnections: maxConnections,
		maxChunk:       maxChunk,
		maxFiles:       maxFiles,
		maxInFlight:    inFlight,
		memoryBudget:   budget,
		fixed:          opts.Fixed,
	}
}

// Initial returns the starting limits for a transfer.
func (c *Adaptive) Initial(profile SystemProfile, available int) Limits {
	c.mu.Lock()
	defer c.mu.Unlock()
	if profile.CPUs > 0 && profile.CPUs < c.limits.Connections {
		c.limits.Connections = max(1, profile.CPUs)
	}
	if c.fixed {
		return c.limits
	}
	return c.clampLocked(c.limits)
}

// Current returns the most recently computed limits.
func (c *Adaptive) Current() Limits {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limits
}

// Observe updates the controller with new measurements and returns the new
// limits. It is safe to call from a single sampler goroutine.
func (c *Adaptive) Observe(s Sample) Limits {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fixed {
		return c.limits
	}
	if s.Interval <= 0 {
		s.Interval = 500 * time.Millisecond
	}
	instant := float64(s.Bytes) / s.Interval.Seconds()
	c.bw = ewma(c.bw, instant, 0.3)
	if s.RTT > 0 {
		if c.baseRTT == 0 || s.RTT < c.baseRTT {
			c.baseRTT = s.RTT
		}
		c.rtt = ewmaDuration(c.rtt, s.RTT, 0.2)
	}
	bad := false
	if c.baseRTT > 0 && c.rtt > 2*c.baseRTT {
		bad = true
	}
	if c.lastRetrans > 0 && s.Retransmits > c.lastRetrans && s.Retransmits-c.lastRetrans > 4 {
		bad = true
	}
	c.lastRetrans = s.Retransmits
	if s.SendBlocked > s.Interval/3 {
		bad = true
	}
	if s.Errors > 0 {
		bad = true
	}
	if bad {
		c.limits.ChunkSize = max(minChunkSize, c.limits.ChunkSize/2)
		c.limits.InFlight = max(1, c.limits.InFlight*3/4)
		c.limits.Connections = max(1, c.limits.Connections*3/4)
		c.goodSamples = 0
		c.cooldownUntil = s.At.Add(2 * time.Second)
		return c.clampLocked(c.limits)
	}
	if s.CPUUtilization > 0.85 || s.MemUtilization > 0.90 ||
		(s.MaxQueue > 0 && s.QueueDepth > s.MaxQueue/2) ||
		(!s.At.IsZero() && s.At.Before(c.cooldownUntil)) {
		c.goodSamples = 0
		return c.clampLocked(c.limits)
	}
	c.goodSamples++
	if c.goodSamples < 2 {
		return c.clampLocked(c.limits)
	}
	target := int64(c.bw * c.rtt.Seconds())
	target = clampInt64(target, minWindow, maxWindow)
	switch {
	case c.limits.Connections < c.maxConnections:
		c.limits.Connections++
	case c.limits.InFlight < c.maxInFlight:
		c.limits.InFlight++
	default:
		want := nextPow2(target / int64(max(1, c.limits.Connections*c.limits.InFlight)))
		if want > c.limits.ChunkSize {
			c.limits.ChunkSize = min(want, c.maxChunk)
		}
	}
	c.goodSamples = 0
	if !s.At.IsZero() {
		c.cooldownUntil = s.At.Add(time.Second)
	}
	return c.clampLocked(c.limits)
}

func (c *Adaptive) clampLocked(l Limits) Limits {
	if l.Connections < 1 {
		l.Connections = 1
	}
	if l.Connections > c.maxConnections {
		l.Connections = c.maxConnections
	}
	if l.ChunkSize < minChunkSize {
		l.ChunkSize = minChunkSize
	}
	if l.ChunkSize > c.maxChunk {
		l.ChunkSize = c.maxChunk
	}
	if l.InFlight < 1 {
		l.InFlight = 1
	}
	if l.InFlight > c.maxInFlight {
		l.InFlight = c.maxInFlight
	}
	if l.ParallelFiles < 1 {
		l.ParallelFiles = 1
	}
	if l.ParallelFiles > c.maxFiles {
		l.ParallelFiles = c.maxFiles
	}
	// Enforce the memory budget: connections * in-flight * chunk * 2 (read and
	// encrypted buffers) must stay below the budget.
	for l.Connections*l.InFlight*int(l.ChunkSize)*2 > int(c.memoryBudget) && l.ChunkSize > minChunkSize {
		l.ChunkSize /= 2
	}
	c.limits = l
	return l
}

func ewma(previous, sample, alpha float64) float64 {
	if previous == 0 {
		return sample
	}
	return previous*(1-alpha) + sample*alpha
}

func ewmaDuration(previous, sample time.Duration, alpha float64) time.Duration {
	if previous == 0 {
		return sample
	}
	return time.Duration(float64(previous)*(1-alpha) + float64(sample)*alpha)
}

func clampInt64(v, low, high int64) int64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func nextPow2(v int64) int64 {
	if v <= 1 {
		return 1
	}
	return int64(math.Pow(2, math.Ceil(math.Log2(float64(v)))))
}
