package tcp

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestAdmissionLimiterEnforcesSourceAndRoomWindows(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)

	t.Run("source", func(t *testing.T) {
		limiter := newAdmissionLimiter(2, 20, time.Minute)
		now := base
		limiter.now = func() time.Time { return now }
		if !limiter.allow("192.0.2.10", "room-a") || !limiter.allow("192.0.2.10", "room-b") {
			t.Fatal("admissions below the per-source limit were rejected")
		}
		if limiter.allow("192.0.2.10", "room-c") {
			t.Fatal("admission above the per-source limit was accepted")
		}
		if !limiter.allow("192.0.2.11", "room-c") {
			t.Fatal("one source exhausted another source's allowance")
		}
		now = now.Add(time.Minute + time.Nanosecond)
		if !limiter.allow("192.0.2.10", "room-c") {
			t.Fatal("source allowance did not recover after the window")
		}
	})

	t.Run("room", func(t *testing.T) {
		limiter := newAdmissionLimiter(20, 2, time.Minute)
		limiter.now = func() time.Time { return base }
		if !limiter.allow("192.0.2.20", "target") || !limiter.allow("192.0.2.21", "target") {
			t.Fatal("admissions below the per-room limit were rejected")
		}
		if limiter.allow("192.0.2.22", "target") {
			t.Fatal("admission above the per-room limit was accepted")
		}
		if !limiter.allow("192.0.2.22", "other") {
			t.Fatal("one room exhausted another room's allowance")
		}
	})
}

func TestRoomDenialsStillConsumeTheSourceAllowance(t *testing.T) {
	limiter := newAdmissionLimiter(2, 1, time.Minute)
	limiter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	if !limiter.allow("192.0.2.1", "full") {
		t.Fatal("initial room join was rejected")
	}
	if limiter.allow("192.0.2.2", "full") {
		t.Fatal("full room join was accepted")
	}
	if !limiter.allow("192.0.2.2", "other") {
		t.Fatal("second source attempt should remain below its source limit")
	}
	if limiter.allow("192.0.2.2", "third") {
		t.Fatal("room denial did not consume the source allowance")
	}
}

func TestRelayAdmissionLimitsAreEnforcedOnTheWire(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		_, address, stopServer := startConfiguredTestServer(t,
			WithAdmissionLimits(1, 20, time.Minute),
		)
		defer stopServer()
		first, _, _, err := ConnectToTCPServer(address, "pass123", "room-a")
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		second, _, _, err := ConnectToTCPServer(address, "pass123", "room-b")
		if second != nil {
			second.Close()
		}
		if !errors.Is(err, ErrAdmissionLimited) {
			t.Fatalf("second source join error = %v", err)
		}
	})

	t.Run("room", func(t *testing.T) {
		_, address, stopServer := startConfiguredTestServer(t,
			WithAdmissionLimits(20, 1, time.Minute),
		)
		defer stopServer()
		first, _, _, err := ConnectToTCPServer(address, "pass123", "target")
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		second, _, _, err := ConnectToTCPServer(address, "pass123", "target")
		if second != nil {
			second.Close()
		}
		if !errors.Is(err, ErrAdmissionLimited) {
			t.Fatalf("second room join error = %v", err)
		}
	})
}

func TestAdmissionLimiterIsSafeUnderConcurrentJoins(t *testing.T) {
	const limit = 32
	limiter := newAdmissionLimiter(limit, limit, time.Minute)
	limiter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	accepted := make(chan bool, limit*4)
	for range limit * 4 {
		go func() { accepted <- limiter.allow("198.51.100.4", "target") }()
	}
	count := 0
	for range limit * 4 {
		if <-accepted {
			count++
		}
	}
	if count != limit {
		t.Fatalf("accepted %d concurrent joins, want %d", count, limit)
	}
}

func TestCanonicalSourceCoalescesIPv4MappedAddresses(t *testing.T) {
	plain := canonicalSource(&net.TCPAddr{IP: net.ParseIP("192.0.2.5"), Port: 1111})
	mapped := canonicalSource(&net.TCPAddr{IP: net.ParseIP("::ffff:192.0.2.5"), Port: 2222})
	if plain != "192.0.2.5" || mapped != plain {
		t.Fatalf("canonical sources = %q and %q", plain, mapped)
	}
}

func TestWithAdmissionLimits(t *testing.T) {
	server := newDefaultServer()
	if err := WithAdmissionLimits(11, 7, 15*time.Second)(server); err != nil {
		t.Fatal(err)
	}
	if server.sourceJoinLimit != 11 || server.roomJoinLimit != 7 || server.joinLimitWindow != 15*time.Second {
		t.Fatalf("unexpected admission configuration: %+v", server)
	}
	for _, option := range []serverOptsFunc{
		WithAdmissionLimits(0, 1, time.Second),
		WithAdmissionLimits(1, 0, time.Second),
		WithAdmissionLimits(1, 1, 0),
	} {
		if err := option(newDefaultServer()); err == nil {
			t.Fatal("invalid admission configuration was accepted")
		}
	}
}
