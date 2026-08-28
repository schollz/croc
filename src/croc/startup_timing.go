package croc

import (
	"sync"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
)

type startupTiming struct {
	started time.Time
	mu      sync.Mutex
	seen    map[string]time.Duration
}

func (s *startupTiming) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started.IsZero() {
		return
	}
	s.started = time.Now()
	s.seen = map[string]time.Duration{"process-start": 0}
	log.Debug("startup milestone process-start elapsed=0s")
}

func (s *startupTiming) mark(name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	if s.started.IsZero() {
		s.started = time.Now()
	}
	if s.seen == nil {
		s.seen = make(map[string]time.Duration)
	}
	if _, ok := s.seen[name]; ok {
		s.mu.Unlock()
		return
	}
	elapsed := time.Since(s.started)
	s.seen[name] = elapsed
	s.mu.Unlock()
	log.Debugf("startup milestone %s elapsed=%s", name, elapsed)
}

func (s *startupTiming) elapsed(name string) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	elapsed, ok := s.seen[name]
	return elapsed, ok
}

func (c *Client) markStartup(name string) {
	if c != nil {
		c.startup.mark(name)
	}
}
