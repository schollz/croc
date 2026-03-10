package pool

import "time"

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// Relay describes a relay entry exposed by the pool API.
type Relay struct {
	RelayID       string    `json:"relay_id"`
	IPv6          string    `json:"ipv6,omitempty"`
	IPv4          string    `json:"ipv4,omitempty"`
	Ports         []string  `json:"ports"`
	Password      string    `json:"password,omitempty"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// RegisterRequest is the body for POST /register.
type RegisterRequest struct {
	RelayID  string   `json:"relay_id,omitempty"`
	IPv6     string   `json:"ipv6,omitempty"`
	IPv4     string   `json:"ipv4,omitempty"`
	Ports    []string `json:"ports"`
	Password string   `json:"password,omitempty"`
}

// RegisterResponse is the response for POST /register.
type RegisterResponse struct {
	OK      bool   `json:"ok"`
	RelayID string `json:"relay_id"`
}

// HeartbeatRequest is the body for POST /heartbeat.
type HeartbeatRequest struct {
	RelayID string `json:"relay_id"`
}

// RelayListResponse is the response for POST /relays.
type RelayListResponse struct {
	Relays []Relay `json:"relays"`
}
