// Package redact removes exact secret values from diagnostic text while
// preserving error identity for errors.Is and errors.As callers.
package redact

import (
	"sort"
	"strings"
)

const replacement = "[REDACTED]"

// String replaces each non-empty secret, longest first.
func String(value string, secrets ...string) string {
	filtered := make([]string, 0, len(secrets))
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if _, exists := seen[secret]; exists {
			continue
		}
		seen[secret] = struct{}{}
		filtered = append(filtered, secret)
	}
	sort.Slice(filtered, func(i, j int) bool { return len(filtered[i]) > len(filtered[j]) })
	for _, secret := range filtered {
		value = strings.ReplaceAll(value, secret, replacement)
	}
	return value
}

type sanitizedError struct {
	err  error
	text string
}

func (e sanitizedError) Error() string { return e.text }
func (e sanitizedError) Unwrap() error { return e.err }

// Error redacts an error's display text without losing its wrapped identity.
func Error(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	text := String(err.Error(), secrets...)
	if text == err.Error() {
		return err
	}
	return sanitizedError{err: err, text: text}
}
