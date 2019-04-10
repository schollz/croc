package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Bytes(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		before uint64
		add    uint64
		after  uint64
	}{
		{
			before: 0,
			add:    0,
			after:  0,
		},
		{
			before: 0,
			add:    1,
			after:  1,
		},
		{
			before: 1,
			add:    10,
			after:  11,
		},
	}

	s := New()
	for _, cur := range tests {
		assert.Equal(cur.before, s.Bytes())
		s.AddBytes(cur.add)
		assert.Equal(cur.after, s.Bytes())
	}
}
