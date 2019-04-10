package stats

import "time"

// Duration returns the 'stop - start' duration, if stopped
// Returns 0 if not started
// Returns time.Since(s.timeStart) if not stopped
func (s *Stats) Duration() time.Duration {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.timeStart.IsZero() {
		return 0
	} else if s.timeStop.IsZero() {
		return time.Since(s.timeStart) - s.timePaused
	}
	return s.timeStop.Sub(s.timeStart) - s.timePaused
}

// Bandwidth returns the IO speed in MB/s
func (s *Stats) Bandwidth() float64 {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return (float64(s.nbBytes) / 1024 / 1024) / s.Duration().Seconds()
}
