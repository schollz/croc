//go:build croc_no_tailcat || (!linux && !windows && !darwin && !freebsd && !openbsd)

package tailcattransport

import (
	"context"
	"errors"
	"testing"
)

func TestRelayOnlyTransportIsUnavailable(t *testing.T) {
	if Available() {
		t.Fatal("relay-only build reported Tailcat as available")
	}
	config := Config{StreamCount: MinStreamCount}
	if _, err := Prepare(context.Background(), config); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Prepare error = %v; want ErrUnsupported", err)
	}
	if _, err := Listen(context.Background(), []byte("session-key"), config, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Listen error = %v; want ErrUnsupported", err)
	}
	if _, err := Dial(context.Background(), "offer", []byte("session-key"), config, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Dial error = %v; want ErrUnsupported", err)
	}
}
