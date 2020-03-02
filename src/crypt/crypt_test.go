package crypt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func BenchmarkEncrypt(b *testing.B) {
	bob, _, _ := New([]byte("password"), nil)
	for i := 0; i < b.N; i++ {
		Encrypt([]byte("hello, world"), bob)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key, _, _ := New([]byte("password"), nil)
	msg := []byte("hello, world")
	enc, _ := Encrypt(msg, key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decrypt(enc, key)
	}
}

func TestEncryption(t *testing.T) {
	key, salt, err := New([]byte("password"), nil)
	assert.Nil(t, err)
	msg := []byte("hello, world")
	enc, err := Encrypt(msg, key)
	assert.Nil(t, err)
	dec, err := Decrypt(enc, key)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, err := New([]byte("password"), salt)
	dec, err = Decrypt(enc, key2)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, err = New([]byte("wrong password"), salt)
	dec, err = Decrypt(enc, key2)
	assert.NotNil(t, err)
	assert.NotEqual(t, msg, dec)

	// error with no password
	dec, err = Decrypt([]byte(""), key)
	assert.NotNil(t, err)

	// error with small password
	_, _, err = New([]byte(""), nil)
	assert.NotNil(t, err)
}
