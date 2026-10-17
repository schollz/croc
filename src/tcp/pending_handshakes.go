package tcp

import "sync"

// pendingHandshakeLimiter reserves total and per-source capacity together.
// Its source map contains only connections still performing a handshake.
type pendingHandshakeLimiter struct {
	mu           sync.Mutex
	pending      int
	sources      map[string]int
	maxPending   int
	maxPerSource int
}

func newPendingHandshakeLimiter(maxPending, maxPerSource int) *pendingHandshakeLimiter {
	return &pendingHandshakeLimiter{
		sources:      make(map[string]int),
		maxPending:   maxPending,
		maxPerSource: maxPerSource,
	}
}

func (l *pendingHandshakeLimiter) acquire(source string) (release func(), rejection string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending >= l.maxPending {
		return nil, "global pending handshake limit reached"
	}
	if l.sources[source] >= l.maxPerSource {
		return nil, "source pending handshake limit reached"
	}
	l.pending++
	l.sources[source]++
	return sync.OnceFunc(func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.pending--
		l.sources[source]--
		if l.sources[source] == 0 {
			delete(l.sources, source)
		}
	}), ""
}
