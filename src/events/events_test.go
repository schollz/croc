package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDisabledEmitIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	Enable(&buf)
	Disable := func() {
		mu.Lock()
		active = nil
		mu.Unlock()
	}
	Disable()
	Emit(VersionEvent{Type: TypeVersion, Version: "1.2.3"})
	if buf.Len() != 0 {
		t.Fatalf("expected no output while disabled, got %q", buf.String())
	}
}

func TestEventsEmitValidLines(t *testing.T) {
	var buf bytes.Buffer
	Enable(&buf)
	Version("10.7.0")
	Code("alpha-forest-42")
	Phase("connecting", "connecting to relay")
	Progress(ProgressEvent{
		File:             "file.zip",
		BytesTransferred: 1048576,
		BytesTotal:       10485760,
		Percent:          10,
		SpeedBps:         524288,
	})
	Complete([]CompleteFile{{Name: "file.zip", Bytes: 10485760}})
	Store("https://getcroc.com/s/a", "croc-store-v1-token", "2026-07-30T03:00:00Z", "abc123")
	Error(errors.New("could not connect to relay: refused"))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 event lines, got %d: %q", len(lines), buf.String())
	}

	typeField := func(line string) string {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		typ, _ := raw["type"].(string)
		return typ
	}
	wantTypes := []string{TypeVersion, TypeCode, TypePhase, TypeProgress, TypeComplete, TypeStore, TypeError}
	for i, line := range lines {
		if typ := typeField(line); typ != wantTypes[i] {
			t.Errorf("line %d: type = %q, want %q", i, typ, wantTypes[i])
		}
	}

	var progress ProgressEvent
	if err := json.Unmarshal([]byte(lines[3]), &progress); err != nil {
		t.Fatalf("progress unmarshal: %v", err)
	}
	if progress.BytesTransferred != 1048576 || progress.BytesTotal != 10485760 || progress.SpeedBps != 524288 {
		t.Errorf("progress fields wrong: %+v", progress)
	}
	if progress.Percent != 10 {
		t.Errorf("percent = %v, want 10", progress.Percent)
	}

	var store StoreEvent
	if err := json.Unmarshal([]byte(lines[5]), &store); err != nil {
		t.Fatalf("store unmarshal: %v", err)
	}
	if store.URL != "https://getcroc.com/s/a" || store.Token != "croc-store-v1-token" || store.ExpiresAt != "2026-07-30T03:00:00Z" || store.RevokeID != "abc123" {
		t.Errorf("store fields wrong: %+v", store)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"could not connect to 127.0.0.1:9009: connection refused", "relay_unreachable"},
		{"bad password", "auth_failed"},
		{"message authentication failed", "auth_failed"},
		{"context deadline exceeded", "timed_out"},
		{"could not secure channel", "handshake_failed"},
		{"something unexpected", "failed"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		Enable(&buf)
		Error(errors.New(c.message))
		var errEvent ErrorEvent
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &errEvent); err != nil {
			t.Fatalf("unmarshal %q: %v", buf.String(), err)
		}
		if errEvent.Code != c.want {
			t.Errorf("message %q: code = %q, want %q", c.message, errEvent.Code, c.want)
		}
		if errEvent.Message != c.message {
			t.Errorf("message field = %q, want %q", errEvent.Message, c.message)
		}
	}
}
