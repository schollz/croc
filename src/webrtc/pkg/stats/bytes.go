package stats

// Bytes returns the stored number of bytes
func (s *Stats) Bytes() uint64 {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.nbBytes
}

// AddBytes increase the nbBytes counter
func (s *Stats) AddBytes(c uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.nbBytes += c
}
