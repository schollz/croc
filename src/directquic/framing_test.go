package directquic

import (
	"bytes"
	"io"
	"testing"
)

func TestFrameAndStreamHeaderRoundTrip(t *testing.T) {
	sessionID := bytes.Repeat([]byte{7}, 32)
	wantHeader := StreamHeader{SessionID: sessionID, FileIndex: 3, Lane: 1, LaneCount: 4}
	wantPayload := bytes.Repeat([]byte("payload"), 100)

	var wire bytes.Buffer
	if err := WriteStreamHeader(&wire, wantHeader); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(&wire, wantPayload); err != nil {
		t.Fatal(err)
	}
	header, err := ReadStreamHeader(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if header.FileIndex != wantHeader.FileIndex || header.Lane != wantHeader.Lane || header.LaneCount != wantHeader.LaneCount || !bytes.Equal(header.SessionID, sessionID) {
		t.Fatalf("unexpected header: %#v", header)
	}
	payload, err := ReadFrame(&wire, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, wantPayload) {
		t.Fatal("frame payload changed")
	}
	if _, err = ReadFrame(&wire, payload); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestFrameRejectsInvalidSizesAndHeaders(t *testing.T) {
	if err := WriteFrame(io.Discard, nil); err == nil {
		t.Fatal("empty frame accepted")
	}
	if err := WriteFrame(io.Discard, make([]byte, MaxFrameSize+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := WriteStreamHeader(io.Discard, StreamHeader{SessionID: make([]byte, 32), Lane: 1, LaneCount: 1}); err == nil {
		t.Fatal("invalid lane accepted")
	}

	invalidFrame := []byte{1, 0, 1, 0}
	if _, err := ReadFrame(bytes.NewReader(invalidFrame), nil); err == nil {
		t.Fatal("oversized frame header accepted")
	}
}
