// Package events emits newline-delimited JSON events for machine
// consumption (--json). The human-oriented output is left untouched.
//
// Event emission is off until Enable is called, so existing callers
// and tests that never opt in are unaffected.
package events

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// Event types.
const (
	TypeVersion  = "version"
	TypeCode     = "code"
	TypePhase    = "phase"
	TypeProgress = "progress"
	TypeComplete = "complete"
	TypeStore    = "store"
	TypeError    = "error"
)

// VersionEvent is emitted once at startup.
type VersionEvent struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

// CodeEvent reports the code phrase the recipient needs to join.
type CodeEvent struct {
	Type string `json:"type"`
	Code string `json:"code"`
}

// PhaseEvent reports a transfer phase transition.
type PhaseEvent struct {
	Type    string `json:"type"`
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
}

// ProgressEvent reports transfer progress for the current file.
type ProgressEvent struct {
	Type             string  `json:"type"`
	File             string  `json:"file"`
	BytesTransferred int64   `json:"bytesTransferred"`
	BytesTotal       int64   `json:"bytesTotal"`
	Percent          float64 `json:"percent"`
	SpeedBps         int64   `json:"speedBps"`
}

// CompleteFile describes a single transferred file in a CompleteEvent.
type CompleteFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// CompleteEvent is emitted when the transfer finishes successfully.
type CompleteEvent struct {
	Type  string         `json:"type"`
	Files []CompleteFile `json:"files"`
}

// StoreEvent is emitted for stored transfers with the share details.
type StoreEvent struct {
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	RevokeID  string `json:"revokeId,omitempty"`
}

// ErrorEvent is emitted when the command fails.
type ErrorEvent struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

var (
	mu     sync.Mutex
	active *json.Encoder
)

// Enable turns on event emission, writing newline-delimited JSON to w.
func Enable(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		active = nil
		return
	}
	active = json.NewEncoder(w)
}

// Enabled reports whether event emission is on.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return active != nil
}

// Emit writes a single event. It is a no-op unless Enable was called.
func Emit(v any) {
	mu.Lock()
	defer mu.Unlock()
	if active != nil {
		_ = active.Encode(v)
	}
}

// Version emits the version event.
func Version(version string) {
	Emit(VersionEvent{Type: TypeVersion, Version: version})
}

// Code emits the code phrase event.
func Code(code string) {
	Emit(CodeEvent{Type: TypeCode, Code: code})
}

// Phase emits a phase transition event.
func Phase(phase, message string) {
	Emit(PhaseEvent{Type: TypePhase, Phase: phase, Message: message})
}

// Progress emits a transfer progress event.
func Progress(progress ProgressEvent) {
	progress.Type = TypeProgress
	Emit(progress)
}

// Complete emits the transfer completion event.
func Complete(files []CompleteFile) {
	Emit(CompleteEvent{Type: TypeComplete, Files: files})
}

// Store emits a stored-transfer details event.
func Store(url, token, expiresAt, revokeID string) {
	Emit(StoreEvent{
		Type:      TypeStore,
		URL:       url,
		Token:     token,
		ExpiresAt: expiresAt,
		RevokeID:  revokeID,
	})
}

// Error emits an error event for err, mapping the message to a stable code.
func Error(err error) {
	code, hint := classify(err)
	Emit(ErrorEvent{Type: TypeError, Code: code, Message: err.Error(), Hint: hint})
}

func classify(err error) (code, hint string) {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "could not connect"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "relay unreachable"):
		return "relay_unreachable", "check --relay and your proxy settings"
	case strings.Contains(msg, "bad password"),
		strings.Contains(msg, "message authentication failed"),
		strings.Contains(msg, "passphrase"):
		return "auth_failed", "make sure both sides use the same code phrase and relay password"
	case strings.Contains(msg, "timed out"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return "timed_out", "check your network connection and try again"
	case strings.Contains(msg, "could not secure channel"),
		strings.Contains(msg, "handshake"):
		return "handshake_failed", "the code phrase may be wrong, or the relay may be unreachable"
	default:
		return "failed", ""
	}
}
