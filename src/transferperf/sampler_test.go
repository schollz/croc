package transferperf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProcessSamplerBounds(t *testing.T) {
	sampler := NewProcessSampler(SystemProfile{CPUs: 2, MemoryLimit: 1 << 30})
	now := time.Now()
	cpu, mem := sampler.Sample(now)
	assert.GreaterOrEqual(t, cpu, 0.0)
	assert.LessOrEqual(t, cpu, 1.0)
	assert.GreaterOrEqual(t, mem, 0.0)
	assert.LessOrEqual(t, mem, 1.0)

	deadline := now.Add(20 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = time.Now()
	}
	cpu, mem = sampler.Sample(now.Add(20 * time.Millisecond))
	assert.GreaterOrEqual(t, cpu, 0.0)
	assert.LessOrEqual(t, cpu, 1.0)
	assert.GreaterOrEqual(t, mem, 0.0)
	assert.LessOrEqual(t, mem, 1.0)
}

func TestProcessSamplerZeroValues(t *testing.T) {
	var sampler *ProcessSampler
	cpu, mem := sampler.Sample(time.Now())
	assert.Zero(t, cpu)
	assert.Zero(t, mem)
}
