package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_ControlFlow(t *testing.T) {
	assert := assert.New(t)
	s := New()

	// Everything should be 0 at the beginning
	assert.Equal(true, s.timeStart.IsZero())
	assert.Equal(true, s.timeStop.IsZero())
	assert.Equal(true, s.timePause.IsZero())

	// Should not do anything
	s.Stop()
	assert.Equal(true, s.timeStop.IsZero())

	// Should not do anything
	s.Pause()
	assert.Equal(true, s.timePause.IsZero())

	// Should start
	s.Start()
	originalStart := s.timeStart
	assert.Equal(false, s.timeStart.IsZero())

	// Should pause
	s.Pause()
	assert.Equal(false, s.timePause.IsZero())
	originalPause := s.timePause
	// Should not modify
	s.Pause()
	assert.Equal(originalPause, s.timePause)

	// Should release
	assert.Equal(int64(0), s.timePaused.Nanoseconds())
	s.Start()
	assert.NotEqual(0, s.timePaused.Nanoseconds())
	originalPausedDuration := s.timePaused
	assert.Equal(true, s.timePause.IsZero())
	assert.Equal(originalStart, s.timeStart)

	s.Pause()
	time.Sleep(10 * time.Nanosecond)
	s.Start()
	assert.Equal(true, s.timePaused > originalPausedDuration)

	s.Stop()
	assert.Equal(false, s.timeStop.IsZero())
}
