package stats

import "time"

// Start stores the "start" timestamp
func (s *Stats) Start() {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.timeStart.IsZero() {
		s.timeStart = time.Now()
	} else if !s.timePause.IsZero() {
		s.timePaused += time.Since(s.timePause)
		// Reset
		s.timePause = time.Time{}
	}
}

// Pause stores an interruption timestamp
func (s *Stats) Pause() {
	s.lock.RLock()

	if s.timeStart.IsZero() || !s.timeStop.IsZero() {
		// Can't stop if not started, or if stopped
		s.lock.RUnlock()
		return
	}
	s.lock.RUnlock()

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.timePause.IsZero() {
		s.timePause = time.Now()
	}
}

// Stop stores the "stop" timestamp
func (s *Stats) Stop() {
	s.lock.RLock()

	if s.timeStart.IsZero() {
		// Can't stop if not started
		s.lock.RUnlock()
		return
	}
	s.lock.RUnlock()

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.timeStop.IsZero() {
		s.timeStop = time.Now()
	}
}
