package tcp

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// readFromRecorder wraps a net.Conn and records whether the ReadFrom fast path
// (io.Copy's zero-copy splice(2) optimization) is selected on this platform.
type readFromRecorder struct {
	net.Conn
	readFromCalled bool
	written        int
}

func (r *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	n, err := io.Copy(io.Discard, src)
	return n, err
}

func (r *readFromRecorder) Write(p []byte) (int, error) {
	n, err := r.Conn.Write(p)
	r.written += n
	return n, err
}

// TestIOCopyWouldTakeFastPath documents the hazard that relayCopy avoids:
// plain io.Copy on the same wrapper does select ReadFrom on Linux (splice(2)).
func TestIOCopyWouldTakeFastPath(t *testing.T) {
	src, dst := net.Pipe()
	go io.Copy(io.Discard, dst)
	rec := &readFromRecorder{Conn: dst}
	go func() {
		src.Write(bytes.Repeat([]byte("x"), 8*1024))
		src.Close()
	}()
	io.Copy(rec, src)
	if !rec.readFromCalled {
		t.Skip("platform does not select the ReadFrom fast path")
	}
	dst.Close()
}

// TestRelayCopyAvoidsReadFromFastPath guards schollz/croc#1359: relay data
// pipes must not rely on io.Copy's ReadFrom (splice(2) on Linux) fast path,
// which aborts long-lived relay transfers with "splice: connection reset by
// peer" and stops further relay debug logging. relayCopy uses an explicit
// buffered loop instead.
func TestRelayCopyAvoidsReadFromFastPath(t *testing.T) {
	src, dst := net.Pipe()
	rec := &readFromRecorder{Conn: dst}

	// free the synchronous pipe by consuming forwarded bytes
	go func() {
		buf := make([]byte, 32*1024)
		for {
			if _, err := rec.Conn.Read(buf); err != nil {
				return
			}
		}
	}()

	const total = 128 * 1024
	go func() {
		src.Write(bytes.Repeat([]byte("x"), total))
		src.Close()
	}()

	done := make(chan struct{})
	go func() { relayCopy(rec, src); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("relayCopy stalled")
	}

	if rec.readFromCalled {
		t.Fatal("relayCopy selected the ReadFrom/splice fast path; relay pipes must use an explicit buffered copy loop (schollz/croc#1359)")
	}
	dst.Close()
}
