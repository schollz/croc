package box

import (
	"testing"

	"github.com/schollz/croc/v7/src/crypt"
	"github.com/stretchr/testify/assert"
)

type M struct {
	Message string
}

func BenchmarkBundle(b *testing.B) {
	key, _, _ := crypt.New([]byte("password"), nil)
	for i := 0; i < b.N; i++ {
		Bundle(M{"hello, world"}, key)
	}
}

func BenchmarkUnbundle(b *testing.B) {
	key, _, _ := crypt.New([]byte("password"), nil)
	bundled, _ := Bundle(M{"hello, world"}, key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var m M
		Unbundle(bundled, key, &m)
	}
}

func TestBox(t *testing.T) {
	key, _, _ := crypt.New([]byte("password"), nil)

	bundled, err := Bundle(M{"hello, world"}, key)
	assert.Nil(t, err)

	var m M
	err = Unbundle(bundled, key, &m)
	assert.Nil(t, err)
	assert.Equal(t, "hello, world", m.Message)
}
