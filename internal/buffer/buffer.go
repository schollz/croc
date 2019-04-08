package buffer

import (
	"bytes"
	"sync"
)

// Buffer is a threadsafe buffer
type Buffer struct {
	b bytes.Buffer
	m sync.Mutex
}

// Read in a thread-safe way
func (b *Buffer) Read(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Read(p)
}

// ReadString in a thread-safe way
func (b *Buffer) ReadString(delim byte) (line string, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.ReadString(delim)
}

// Write in a thread-safe way
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}

// WriteString in a thread-safe way
func (b *Buffer) WriteString(s string) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.WriteString(s)
}

// String in a thread-safe way
func (b *Buffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}
