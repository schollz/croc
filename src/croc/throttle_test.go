package croc

import (
	"testing"

	"golang.org/x/time/rate"
)

func TestParseUploadLimit(t *testing.T) {
	valid := map[string]int64{
		"5":    5,
		"500":  500,
		"500k": 500 * 1024,
		"512K": 512 * 1024,
		"10m":  10 * 1024 * 1024,
		"1g":   1024 * 1024 * 1024,
		"2G":   2 * 1024 * 1024 * 1024,
	}
	for value, want := range valid {
		got, err := parseUploadLimit(value)
		if err != nil {
			t.Errorf("parseUploadLimit(%q) returned error: %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("parseUploadLimit(%q) = %d; want %d", value, got, want)
		}
	}

	for _, value := range []string{"abc", "k", "1.5m", "500B", "0", "00", "0k", "-5k", "99999999999g"} {
		if got, err := parseUploadLimit(value); err == nil {
			t.Errorf("parseUploadLimit(%q) = %d; want an error", value, got)
		}
	}
}

func TestNewRejectsInvalidUploadLimit(t *testing.T) {
	for _, value := range []string{"abc", "0k"} {
		_, err := New(Options{
			SharedSecret:   "throttle-invalid-limit",
			IsSender:       true,
			Curve:          "p256",
			ThrottleUpload: value,
		})
		if err == nil {
			t.Errorf("New with ThrottleUpload %q succeeded; want an error", value)
		}
	}
}

func TestNewUploadLimitIsExact(t *testing.T) {
	// time.Second / limit used to truncate to whole nanoseconds, which made
	// large limits much too high and turned 1g into no limit at all.
	for value, want := range map[string]rate.Limit{
		"300k": 300 * 1024,
		"600m": 600 * 1024 * 1024,
		"1g":   1024 * 1024 * 1024,
	} {
		client, err := New(Options{
			SharedSecret:   "throttle-exact-limit",
			IsSender:       true,
			Curve:          "p256",
			ThrottleUpload: value,
		})
		if err != nil {
			t.Fatalf("New with ThrottleUpload %q: %v", value, err)
		}
		if got := client.limiter.Limit(); got != want {
			t.Errorf("ThrottleUpload %q gave limit %v; want %v", value, got, want)
		}
	}
}
