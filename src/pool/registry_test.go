package pool

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRegistryUpsertHeartbeatAndList(t *testing.T) {
	r := NewRegistry()
	r.Upsert(Relay{RelayID: "ab12", IPv4: "203.0.113.10", Ports: []string{"9009", "9010"}})

	relay, ok := r.Get("ab12")
	assert.True(t, ok)
	assert.Equal(t, StatusActive, relay.Status)

	assert.True(t, r.Heartbeat("ab12"))
	assert.False(t, r.Heartbeat("ffff"))

	active := r.ListActive(50)
	assert.Len(t, active, 1)
}

func TestRegistryMarkInactiveOlderThan(t *testing.T) {
	r := NewRegistry()
	r.Upsert(Relay{RelayID: "ab12", IPv4: "203.0.113.10", Ports: []string{"9009", "9010"}})

	r.mu.Lock()
	relay := r.relays["ab12"]
	relay.LastHeartbeat = time.Now().UTC().Add(-2 * time.Minute)
	r.relays["ab12"] = relay
	r.mu.Unlock()

	changed := r.MarkInactiveOlderThan(30 * time.Second)
	assert.Equal(t, 1, changed)

	relay, _ = r.Get("ab12")
	assert.Equal(t, StatusInactive, relay.Status)
}
