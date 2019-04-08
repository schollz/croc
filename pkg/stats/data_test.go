package stats

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Bandwidth(t *testing.T) {
	assert := assert.New(t)
	s := New()

	now := time.Now()
	tests := []struct {
		startTime         time.Time
		stopTime          time.Time
		pauseDuration     time.Duration
		bytesCount        uint64
		expectedBandwidth float64
	}{
		{
			startTime:         time.Time{},
			stopTime:          time.Time{},
			pauseDuration:     0,
			bytesCount:        0,
			expectedBandwidth: math.NaN(),
		},
		{
			startTime:         now,
			stopTime:          time.Time{},
			pauseDuration:     0,
			bytesCount:        0,
			expectedBandwidth: 0,
		},
		{
			startTime:         now,
			stopTime:          now.Add(time.Duration(1 * 1000000000)),
			pauseDuration:     0,
			bytesCount:        1024 * 1024,
			expectedBandwidth: 1,
		},
		{
			startTime:         now,
			stopTime:          now.Add(time.Duration(2 * 1000000000)),
			pauseDuration:     time.Duration(1 * 1000000000),
			bytesCount:        1024 * 1024,
			expectedBandwidth: 1,
		},
	}

	for _, cur := range tests {
		s.timeStart = cur.startTime
		s.timeStop = cur.stopTime
		s.timePaused = cur.pauseDuration
		s.nbBytes = cur.bytesCount

		if math.IsNaN(cur.expectedBandwidth) {
			assert.Equal(true, math.IsNaN(s.Bandwidth()))
		} else {
			assert.Equal(cur.expectedBandwidth, s.Bandwidth())
		}
	}
}

func Test_Duration(t *testing.T) {
	assert := assert.New(t)
	s := New()

	// Should be 0
	assert.Equal(time.Duration(0), s.Duration())

	// Should return time.Since()
	s.Start()
	durationTmp := s.Duration()
	time.Sleep(10 * time.Nanosecond)
	assert.Equal(true, s.Duration() > durationTmp)
}
