//go:build linux && (386 || arm)

package derptransport

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatformStub(t *testing.T) {
	if Available() {
		t.Fatal("DERP unexpectedly available")
	}
	if _, err := Listen(context.Background(), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Listen() error = %v, want %v", err, ErrUnsupported)
	}
	if _, err := Dial(context.Background(), "", nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Dial() error = %v, want %v", err, ErrUnsupported)
	}
}
