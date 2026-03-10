package pool

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRegisterAssignsRelayIDFromIP(t *testing.T) {
	s := NewServer("")

	reqBody := RegisterRequest{
		RelayID:  "ffff", // should be ignored by server
		IPv4:     "203.0.113.10",
		Ports:    []string{"9009", "9010"},
		Password: "pass123",
	}
	b, err := json.Marshal(reqBody)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleRegister(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp RegisterResponse
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.NotEqual(t, "ffff", resp.RelayID)

	expected, err := GenerateRelayIDFromIP("203.0.113.10")
	assert.NoError(t, err)
	assert.Equal(t, expected, resp.RelayID)

	relay, ok := s.Registry.Get(resp.RelayID)
	assert.True(t, ok)
	assert.Equal(t, "203.0.113.10", relay.IPv4)
}

func TestRelaysEndpointReturnsActiveOnly(t *testing.T) {
	s := NewServer("")
	s.Registry.Upsert(Relay{RelayID: "ab12", IPv4: "203.0.113.10", Ports: []string{"9009", "9010"}})
	s.Registry.Upsert(Relay{RelayID: "cd34", IPv4: "198.51.100.8", Ports: []string{"9009", "9010"}})

	// Mark one relay stale so cleanup marks only that relay inactive.
	s.Registry.mu.Lock()
	relay := s.Registry.relays["cd34"]
	relay.LastHeartbeat = time.Now().UTC().Add(-2 * time.Minute)
	s.Registry.relays["cd34"] = relay
	s.Registry.mu.Unlock()
	s.Registry.MarkInactiveOlderThan(30 * time.Second)

	req := httptest.NewRequest(http.MethodPost, "/relays", http.NoBody)
	rr := httptest.NewRecorder()
	s.handleRelays(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp RelayListResponse
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Relays, 1)
	assert.Equal(t, "ab12", resp.Relays[0].RelayID)
	for _, r := range resp.Relays {
		assert.Equal(t, StatusActive, r.Status)
	}
}

func TestRegisterSanitizesPorts(t *testing.T) {
	s := NewServer("")

	reqBody := RegisterRequest{
		IPv4:  "203.0.113.10",
		Ports: []string{" 9009 ", "9010"},
	}
	b, err := json.Marshal(reqBody)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleRegister(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp RegisterResponse
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	relay, ok := s.Registry.Get(resp.RelayID)
	assert.True(t, ok)
	assert.Equal(t, []string{"9009", "9010"}, relay.Ports)
}

func TestRegisterRejectsInvalidPorts(t *testing.T) {
	s := NewServer("")

	reqBody := RegisterRequest{
		IPv4:  "203.0.113.10",
		Ports: []string{"9009", "abc"},
	}
	b, err := json.Marshal(reqBody)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleRegister(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "must be numeric")
}
