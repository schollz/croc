package crypt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func BenchmarkEncryptionNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bob, _ := New([]byte("password"), nil)
		bob.Encrypt([]byte("hello, world"))
	}
}

func BenchmarkEncryption(b *testing.B) {
	bob, _ := New([]byte("password"), nil)
	for i := 0; i < b.N; i++ {
		bob.Encrypt([]byte("hello, world"))
	}
}

func TestEncryption(t *testing.T) {
	bob, err := New([]byte("password"), nil)
	assert.Nil(t, err)
	jane, err := New([]byte("password"), bob.Salt)
	assert.Nil(t, err)
	enc := bob.Encrypt([]byte("hello, world"))
	dec, err := jane.Decrypt(enc)
	assert.Nil(t, err)
	assert.Equal(t, dec, []byte("hello, world"))
}
