package transferperf

import (
	"runtime"
	"time"
)

// ProcessSampler reports process CPU and heap utilization between calls. It is
// not safe for concurrent use; one sampler belongs to one transfer batch.
type ProcessSampler struct {
	cpus        int
	memoryLimit int64
	lastCPU     time.Duration
	lastAt      time.Time
	haveLast    bool
}

// NewProcessSampler creates a sampler from a probed profile.
func NewProcessSampler(profile SystemProfile) *ProcessSampler {
	cpus := profile.CPUs
	if cpus < 1 {
		cpus = runtime.NumCPU()
	}
	limit := profile.MemoryLimit
	if limit <= 0 {
		limit = profile.MemoryBytes
	}
	return &ProcessSampler{cpus: cpus, memoryLimit: limit}
}

// Sample returns CPU utilization (0-1, normalized by CPUs) and heap
// utilization (0-1) since the previous call. Unknown values are zero.
func (s *ProcessSampler) Sample(now time.Time) (cpu, memory float64) {
	if s == nil {
		return 0, 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	if s.cpus < 1 {
		s.cpus = 1
	}
	if cpuTime, ok := processCPUTime(); ok {
		if s.haveLast {
			wall := now.Sub(s.lastAt)
			if wall > 0 {
				delta := cpuTime - s.lastCPU
				if delta > 0 {
					cpu = float64(delta) / float64(wall) / float64(s.cpus)
					cpu = clamp01(cpu)
				}
			}
		}
		s.lastCPU = cpuTime
		s.lastAt = now
		s.haveLast = true
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if s.memoryLimit > 0 {
		memory = clamp01(float64(stats.HeapAlloc) / float64(s.memoryLimit))
	}
	return cpu, memory
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
