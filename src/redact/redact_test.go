package redact

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorRedactsSecretsAndPreservesIdentity(t *testing.T) {
	sentinel := errors.New("sentinel")
	secret := "film-alibi-jet"
	err := fmt.Errorf("connection for %s failed: %w", secret, sentinel)
	got := Error(err, secret, "alibi-jet", "")
	if strings.Contains(got.Error(), secret) || strings.Contains(got.Error(), "alibi-jet") {
		t.Fatalf("secret remained in error: %q", got)
	}
	if !errors.Is(got, sentinel) {
		t.Fatal("redaction lost wrapped error identity")
	}
}

func TestStringRedactsRepeatedAndOverlappingSecrets(t *testing.T) {
	got := String("long-secret then secret then long-secret", "secret", "long-secret")
	if got != "[REDACTED] then [REDACTED] then [REDACTED]" {
		t.Fatalf("redacted string = %q", got)
	}
}
