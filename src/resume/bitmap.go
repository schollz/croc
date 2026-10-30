package resume

import "fmt"

// Bitmap tracks one bit per transfer chunk. Bit i is set when chunk i has
// been written and verified. It is not safe for concurrent use; callers must
// provide their own synchronization.
type Bitmap struct {
	bits  []byte
	count int64
	set   int64
}

// NewBitmap allocates a bitmap for count chunks.
func NewBitmap(count int64) *Bitmap {
	if count < 0 {
		count = 0
	}
	return &Bitmap{bits: make([]byte, (count+7)/8), count: count}
}

// NewBitmapFromBytes validates and adopts a serialized bitmap.
func NewBitmapFromBytes(data []byte, count int64) (*Bitmap, error) {
	if count < 0 {
		return nil, fmt.Errorf("negative chunk count %d", count)
	}
	expected := (count + 7) / 8
	if int64(len(data)) != expected {
		return nil, fmt.Errorf("bitmap length %d does not match chunk count %d", len(data), count)
	}
	if count%8 != 0 && len(data) > 0 {
		unused := byte(0xff << uint(count%8))
		if data[len(data)-1]&unused != 0 {
			return nil, fmt.Errorf("bitmap has stray bits past chunk count")
		}
	}
	b := &Bitmap{bits: append([]byte(nil), data...), count: count}
	for i := range count {
		if b.IsSet(i) {
			b.set++
		}
	}
	return b, nil
}

// Len returns the number of chunks represented by the bitmap.
func (b *Bitmap) Len() int64 { return b.count }

// Bytes returns a copy of the serialized bitmap.
func (b *Bitmap) Bytes() []byte { return append([]byte(nil), b.bits...) }

// IsSet reports whether chunk i is set.
func (b *Bitmap) IsSet(i int64) bool {
	if b == nil || i < 0 || i >= b.count {
		return false
	}
	return b.bits[i/8]&(1<<uint(i%8)) != 0
}

// Set marks chunk i and reports whether the bitmap changed.
func (b *Bitmap) Set(i int64) bool {
	if b == nil || i < 0 || i >= b.count || b.IsSet(i) {
		return false
	}
	b.bits[i/8] |= 1 << uint(i%8)
	b.set++
	return true
}

// Clear removes chunk i and reports whether the bitmap changed.
func (b *Bitmap) Clear(i int64) bool {
	if b == nil || i < 0 || i >= b.count || !b.IsSet(i) {
		return false
	}
	b.bits[i/8] &^= 1 << uint(i%8)
	b.set--
	return true
}

// Count returns the number of set chunks.
func (b *Bitmap) Count() int64 { return b.set }

// ClearAll removes every chunk from the bitmap.
func (b *Bitmap) ClearAll() {
	if b == nil {
		return
	}
	b.bits = make([]byte, len(b.bits))
	b.set = 0
}

// MissingRanges returns ranges in the protocol format
// [chunkSize, startChunk, count, ...]. It returns nil when nothing is missing.
func (b *Bitmap) MissingRanges(chunkSize, fileSize int64) []int64 {
	if b == nil || b.count == 0 || chunkSize <= 0 || fileSize <= 0 {
		return nil
	}
	var ranges []int64
	start := int64(-1)
	var run int64
	flush := func() {
		if run == 0 {
			return
		}
		if len(ranges) == 0 {
			ranges = append(ranges, chunkSize)
		}
		ranges = append(ranges, start, run)
		run = 0
	}
	for i := int64(0); i < b.count; i++ {
		if b.IsSet(i) {
			flush()
			continue
		}
		if run == 0 {
			start = i
		}
		run++
	}
	flush()
	return ranges
}
