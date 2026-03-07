package pool

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"sync"
	"time"

	log "github.com/schollz/logger"
)

// RelayStore manages relay registrations with TTL
type RelayStore struct {
	mu      sync.RWMutex
	relays  map[string]*RelayInfo
	ttl     time.Duration
	cleanup time.Duration
	warmup  time.Duration
}

// NewRelayStore creates a new relay store with the specified TTL
func NewRelayStore(ttl time.Duration) *RelayStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	store := &RelayStore{
		relays:  make(map[string]*RelayInfo),
		ttl:     ttl,
		cleanup: ttl / 5, // Clean up every 20% of TTL
		warmup:  2 * time.Minute,
	}
	if store.cleanup < time.Second {
		store.cleanup = time.Second
	}
	if store.warmup > ttl {
		store.warmup = ttl / 2
	}
	return store
}

// RelayInfo holds information about a registered relay
type RelayInfo struct {
	Address                    string    `json:"address"`
	Version                    string    `json:"version"`
	UptimePercent              float64   `json:"-"`
	SourceIP                   string    `json:"-"`
	Token                      string    `json:"-"`
	FirstSeen                  time.Time `json:"first_seen"`
	LastHeartbeat              time.Time `json:"last_heartbeat"`
	LastValidation             time.Time `json:"last_validation"`
	Status                     string    `json:"status"`
	HeartbeatCount             int       `json:"-"` // Internal counter
	ExpectedBeats              int       `json:"-"` // Expected heartbeats based on time alive
	ConsecutiveValidationFails int       `json:"-"`
	SuccessfulValidationChecks int       `json:"-"`
}

// GetUptimePercent calculates the uptime percentage based on heartbeat consistency
func (r *RelayInfo) GetUptimePercent() float64 {
	if r.UptimePercent > 0 {
		if r.UptimePercent > 100.0 {
			return 100.0
		}
		return r.UptimePercent
	}

	if r.ExpectedBeats == 0 {
		return 100.0 // New relay, assume 100%
	}
	percent := (float64(r.HeartbeatCount) / float64(r.ExpectedBeats)) * 100.0
	if percent > 100.0 {
		percent = 100.0
	}
	return percent
}

// Register adds or updates a relay in the store
func (s *RelayStore) Register(address, version, sourceIP string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	token := generateRelayToken()
	if relay, exists := s.relays[address]; exists {
		// Refresh existing relay registration.
		relay.LastHeartbeat = now
		relay.HeartbeatCount++
		relay.SourceIP = sourceIP
		relay.Token = token
		relay.Status = "pending"
		relay.ConsecutiveValidationFails = 0
		relay.SuccessfulValidationChecks = 0
		if version != "" {
			relay.Version = version
		}
		minutesAlive := int(now.Sub(relay.FirstSeen).Minutes())
		relay.ExpectedBeats = (minutesAlive / 5) + 1
		return token, false
	} else {
		s.relays[address] = &RelayInfo{
			Address:                    address,
			Version:                    version,
			SourceIP:                   sourceIP,
			Token:                      token,
			FirstSeen:                  now,
			LastHeartbeat:              now,
			Status:                     "pending",
			HeartbeatCount:             1,
			ExpectedBeats:              1,
			ConsecutiveValidationFails: 0,
			SuccessfulValidationChecks: 0,
		}
		return token, true
	}
}

// Heartbeat refreshes the TTL for an existing relay if token is valid.
func (s *RelayStore) Heartbeat(address, token, sourceIP string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if relay, exists := s.relays[address]; exists {
		if relay.Token != token || relay.SourceIP != sourceIP {
			return false, false
		}

		now := time.Now()
		relay.LastHeartbeat = now
		relay.HeartbeatCount++

		minutesAlive := int(now.Sub(relay.FirstSeen).Minutes())
		relay.ExpectedBeats = (minutesAlive / 5) + 1

		return true, relay.Status == "active"
	}
	return false, false
}

// GetRelays returns a list of non-expired relays including warmup and ready states.
func (s *RelayStore) GetRelays() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	result := make([]map[string]interface{}, 0, len(s.relays))
	for _, relay := range s.relays {
		if now.Sub(relay.LastHeartbeat) >= s.ttl {
			continue
		}

		displayStatus := "warming_up"
		ready := false
		if relay.Status == "active" {
			displayStatus = "ready"
			ready = true
		} else if now.Sub(relay.FirstSeen) < s.warmup {
			displayStatus = "warming_up"
		} else {
			// Still pending after warmup means probe validation hasn't passed
			displayStatus = "pending_validation"
		}

		result = append(result, map[string]interface{}{
			"address":       relay.Address,
			"version":       relay.Version,
			"uptimePercent": relay.GetUptimePercent(),
			"lastHeartbeat": relay.LastHeartbeat,
			"warmupEndsAt":  relay.FirstSeen.Add(s.warmup),
			"status":        displayStatus,
			"ready":         ready,
		})
	}
	return result
}

// Cleanup removes expired relays (those that haven't sent a heartbeat within the TTL)
func (s *RelayStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for address, relay := range s.relays {
		if now.Sub(relay.LastHeartbeat) >= s.ttl {
			delete(s.relays, address)
			removed++
		}
	}

	return removed
}

// Count returns the number of active relays
func (s *RelayStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	count := 0
	for _, relay := range s.relays {
		if now.Sub(relay.LastHeartbeat) < s.ttl && relay.Status == "active" {
			count++
		}
	}

	return count
}

// ProbeRelays validates relay connectivity and promotes healthy pending relays.
func (s *RelayStore) ProbeRelays(timeout time.Duration) {
	type relaySnapshot struct {
		address string
	}

	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	now := time.Now()
	s.mu.RLock()
	snapshots := make([]relaySnapshot, 0, len(s.relays))
	for _, relay := range s.relays {
		if now.Sub(relay.LastHeartbeat) < s.ttl {
			snapshots = append(snapshots, relaySnapshot{address: relay.Address})
		}
	}
	s.mu.RUnlock()

	for _, snap := range snapshots {
		conn, err := net.DialTimeout("tcp", snap.address, timeout)
		s.mu.Lock()
		relay, exists := s.relays[snap.address]
		if !exists {
			s.mu.Unlock()
			if conn != nil {
				_ = conn.Close()
			}
			continue
		}

		relay.LastValidation = time.Now()
		if err != nil {
			relay.ConsecutiveValidationFails++
			if relay.ConsecutiveValidationFails >= 3 {
				delete(s.relays, snap.address)
			}
			s.mu.Unlock()
			continue
		}

		relay.ConsecutiveValidationFails = 0
		relay.SuccessfulValidationChecks++
		if now.Sub(relay.FirstSeen) >= s.warmup && relay.SuccessfulValidationChecks >= 2 {
			relay.Status = "active"
		}
		s.mu.Unlock()
		_ = conn.Close()
	}
}

// StartCleanupLoop runs a background goroutine that cleans up expired relays
func (s *RelayStore) StartCleanupLoop(interval time.Duration, stopCh <-chan struct{}) {
	if interval <= 0 {
		interval = time.Minute
	}

	// Immediate pass so stale entries disappear promptly on startup.
	_ = s.Cleanup()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.ProbeRelays(2 * time.Second)
			removed := s.Cleanup()
			if removed > 0 {
				log.Debugf("Cleaned up %d expired relays", removed)
			}
		case <-stopCh:
			return
		}
	}
}

func generateRelayToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}
