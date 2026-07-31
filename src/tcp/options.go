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
