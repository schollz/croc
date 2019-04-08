package stats

import (
	"fmt"
	"sync"
	"time"
)

// Stats provide a way to track statistics infos
type Stats struct {
	lock      *sync.RWMutex
	nbBytes   uint64
	timeStart time.Time
	timeStop  time.Time

	timePause  time.Time
	timePaused time.Duration
}

// New creates a new Stats
func New() *Stats {
	return &Stats{
		lock: &sync.RWMutex{},
	}
}

func (s *Stats) String() string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return fmt.Sprintf("%v bytes | %-v | %0.4f MB/s", s.Bytes(), s.Duration(), s.Bandwidth())
}
