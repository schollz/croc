package tcp

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

type admissionLimiter struct {
	mu          sync.Mutex
	sources     map[string][]time.Time
	rooms       map[string][]time.Time
	sourceLimit int
	roomLimit   int
	window      time.Duration
	now         func() time.Time
	checks      uint64
}

func newAdmissionLimiter(sourceLimit, roomLimit int, window time.Duration) *admissionLimiter {
	return &admissionLimiter{
		sources:     make(map[string][]time.Time),
		rooms:       make(map[string][]time.Time),
		sourceLimit: sourceLimit,
		roomLimit:   roomLimit,
		window:      window,
		now:         time.Now,
	}
}

func (l *admissionLimiter) allow(source, room string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	sourceEvents := pruneAdmissions(l.sources[source], cutoff)
	roomEvents := pruneAdmissions(l.rooms[room], cutoff)
	if len(sourceEvents) >= l.sourceLimit {
		l.sources[source] = sourceEvents
		return false
	}
	l.sources[source] = append(sourceEvents, now)
	if len(roomEvents) >= l.roomLimit {
		l.rooms[room] = roomEvents
		return false
	}
	l.rooms[room] = append(roomEvents, now)
	l.checks++
	if l.checks%256 == 0 {
		pruneAdmissionMap(l.sources, cutoff)
		pruneAdmissionMap(l.rooms, cutoff)
	}
	return true
}

func pruneAdmissions(events []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(events) && !events[first].After(cutoff) {
		first++
	}
	return events[first:]
}

func pruneAdmissionMap(entries map[string][]time.Time, cutoff time.Time) {
	for key, events := range entries {
		events = pruneAdmissions(events, cutoff)
		if len(events) == 0 {
			delete(entries, key)
		} else {
			entries[key] = events
		}
	}
}

func canonicalSource(address net.Addr) string {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return tcpAddress.AddrPort().Addr().Unmap().String()
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return address.String()
	}
	if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
		return parsed.Unmap().String()
	}
	return host
}
