package transferperf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestControllerConservativeStartAndClamp(t *testing.T) {
	c := NewController(SystemProfile{CPUs: 8}, 4, Options{MaxFiles: 8})
	initial := c.Initial(SystemProfile{CPUs: 8}, 4)
	assert.Equal(t, 2, initial.Connections)
	assert.Equal(t, int64(256<<10), initial.ChunkSize)
	assert.Equal(t, 2, initial.InFlight)
	assert.Equal(t, 4, initial.ParallelFiles)

	now := time.Now()
	limits := initial
	for i := range 50 {
		limits = c.Observe(Sample{
			At:       now.Add(time.Duration(i) * 500 * time.Millisecond),
			Bytes:    10 << 20,
			Interval: 500 * time.Millisecond,
			RTT:      10 * time.Millisecond,
		})
	}
	assert.LessOrEqual(t, limits.Connections, 4)
	assert.LessOrEqual(t, limits.ChunkSize, int64(4<<20))
	assert.LessOrEqual(t, int(limits.ChunkSize)*limits.Connections*limits.InFlight*2, int(defaultMemory))
}

func TestControllerBacksOffOnCongestion(t *testing.T) {
	c := NewController(SystemProfile{}, 4, Options{})
	c.Initial(SystemProfile{}, 4)
	now := time.Now()
	c.Observe(Sample{At: now, Bytes: 4 << 20, Interval: time.Second, RTT: 10 * time.Millisecond})
	c.Observe(Sample{At: now.Add(time.Second), Bytes: 4 << 20, Interval: time.Second, RTT: 10 * time.Millisecond})
	before := c.Current()
	got := c.Observe(Sample{At: now.Add(2 * time.Second), Bytes: 1 << 20, Interval: time.Second, RTT: 100 * time.Millisecond})
	assert.LessOrEqual(t, got.ChunkSize, before.ChunkSize)
	assert.LessOrEqual(t, got.Connections, before.Connections)
}

func TestControllerFixedMode(t *testing.T) {
	c := NewController(SystemProfile{}, 8, Options{MaxConnections: 4, MaxChunkSize: 1 << 20, Fixed: true})
	limits := c.Initial(SystemProfile{}, 8)
	assert.Equal(t, 2, limits.Connections)
	assert.Equal(t, int64(256<<10), limits.ChunkSize)
	got := c.Observe(Sample{Bytes: 1, Interval: time.Second, RTT: 10 * time.Millisecond, Errors: 1})
	assert.Equal(t, limits, got)
}

func TestNextPow2(t *testing.T) {
	cases := map[int64]int64{0: 1, 1: 1, 2: 2, 3: 4, 1000: 1024, 65536: 65536, 65537: 131072}
	for in, want := range cases {
		assert.Equal(t, want, nextPow2(in), "input %d", in)
	}
}
