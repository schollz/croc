package tcp

import (
	"fmt"
	"time"
)

// TODO: maybe export from logger library?
var availableLogLevels = []string{"info", "error", "warn", "debug", "trace"}

type serverOptsFunc func(s *server) error

func WithBanner(banner ...string) serverOptsFunc {
	return func(s *server) error {
		if len(banner) > 0 {
			s.banner = banner[0]
		}
		return nil
	}
}

func WithLogLevel(level string) serverOptsFunc {
	return func(s *server) error {
		if !containsSlice(availableLogLevels, level) {
			return fmt.Errorf("invalid log level specified: %s", level)
		}
		s.debugLevel = level
		return nil
	}
}

// WithMaxRoomsOpen sets the maximum number of single-occupant rooms that may
// wait for a peer at the same time.
func WithMaxRoomsOpen(maxRoomsOpen int) serverOptsFunc {
	return func(s *server) error {
		if maxRoomsOpen <= 0 {
			return fmt.Errorf("max rooms open must be positive")
		}
		s.maxRoomsOpen = maxRoomsOpen
		return nil
	}
}

// WithMaxPendingHandshakes sets the maximum number of connections that may be
// performing the initial relay handshake at the same time.
func WithMaxPendingHandshakes(maxPendingHandshakes int) serverOptsFunc {
	return func(s *server) error {
		if maxPendingHandshakes <= 0 {
			return fmt.Errorf("max pending handshakes must be positive")
		}
		s.maxPendingHandshakes = maxPendingHandshakes
		return nil
	}
}

// WithHandshakeTimeout sets the absolute time allowed for the initial relay
// handshake to complete.
func WithHandshakeTimeout(timeout time.Duration) serverOptsFunc {
	return func(s *server) error {
		if timeout <= 0 {
			return fmt.Errorf("handshake timeout must be positive")
		}
		s.handshakeTimeout = timeout
		return nil
	}
}

// WithRoomPairedCallback sets a callback invoked after a room's second peer
// has joined and received confirmation. The callback must not block.
func WithRoomPairedCallback(callback func()) serverOptsFunc {
	return func(s *server) error {
		s.roomPaired = callback
		return nil
	}
}

func WithRoomCleanupInterval(interval time.Duration) serverOptsFunc {
	return func(s *server) error {
		s.roomCleanupInterval = interval
		return nil
	}
}

func WithRoomTTL(ttl time.Duration) serverOptsFunc {
	return func(s *server) error {
		s.roomTTL = ttl
		return nil
	}
}

func containsSlice(s []string, e string) bool {
	for _, ss := range s {
		if e == ss {
			return true
		}
	}
	return false
}
