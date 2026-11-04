package resume

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitmapSetClearAndRanges(t *testing.T) {
	b := NewBitmap(10)
	require.Equal(t, int64(10), b.Len())
	assert.Equal(t, int64(0), b.Count())
	require.True(t, b.Set(1))
	require.True(t, b.Set(2))
	require.True(t, b.Set(5))
	require.False(t, b.Set(5))
	assert.Equal(t, int64(3), b.Count())

	ranges := b.MissingRanges(100, 1000)
	assert.Equal(t, []int64{100, 0, 1, 3, 2, 6, 4}, ranges)

	require.True(t, b.Clear(2))
	assert.Equal(t, int64(2), b.Count())

	raw := b.Bytes()
	restored, err := NewBitmapFromBytes(raw, 10)
	require.NoError(t, err)
	assert.Equal(t, b.Count(), restored.Count())
	assert.True(t, restored.IsSet(1))
	assert.False(t, restored.IsSet(2))
}

func TestBitmapAllMissingAndNone(t *testing.T) {
	b := NewBitmap(4)
	assert.Equal(t, []int64{1024, 0, 4}, b.MissingRanges(1024, 4096))
	for i := range int64(4) {
		b.Set(i)
	}
	assert.Nil(t, b.MissingRanges(1024, 4096))
	assert.False(t, b.Set(4))
	assert.False(t, b.IsSet(-1))
}

func TestBitmapRejectsStrayBits(t *testing.T) {
	_, err := NewBitmapFromBytes([]byte{0x80}, 3)
	assert.Error(t, err)
	_, err = NewBitmapFromBytes([]byte{0x01, 0x01}, 3)
	assert.Error(t, err)
}
