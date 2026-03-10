package pool

import (
	"math/rand"
	"sync"
	"time"
)

// Registry stores relay state in memory.
type Registry struct {
	mu     sync.RWMutex
	relays map[string]Relay
}

func NewRegistry() *Registry {
	return &Registry{relays: make(map[string]Relay)}
}

func (r *Registry) Upsert(relay Relay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	relay.Status = StatusActive
	relay.LastHeartbeat = time.Now().UTC()
	r.relays[relay.RelayID] = relay
}

func (r *Registry) Heartbeat(relayID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	relay, ok := r.relays[relayID]
	if !ok {
		return false
	}
	relay.Status = StatusActive
	relay.LastHeartbeat = time.Now().UTC()
	r.relays[relayID] = relay
	return true
}

func (r *Registry) Get(relayID string) (Relay, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	relay, ok := r.relays[relayID]
	return relay, ok
}

func (r *Registry) MarkInactiveOlderThan(timeout time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	count := 0
	for id, relay := range r.relays {
		if now.Sub(relay.LastHeartbeat) > timeout && relay.Status != StatusInactive {
			relay.Status = StatusInactive
			r.relays[id] = relay
			count++
		}
	}
	return count
}

func (r *Registry) ListActive(limit int) []Relay {
	r.mu.RLock()
	relays := make([]Relay, 0, len(r.relays))
	for _, relay := range r.relays {
		if relay.Status == StatusActive {
			relays = append(relays, relay)
		}
	}
	r.mu.RUnlock()

	rand.Shuffle(len(relays), func(i, j int) {
		relays[i], relays[j] = relays[j], relays[i]
	})

	if limit > 0 && len(relays) > limit {
		relays = relays[:limit]
	}
	return relays
}
