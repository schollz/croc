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
	jane, err := New([]byte("password"), bob.Salt())
	assert.Nil(t, err)
	enc, err := bob.Encrypt([]byte("hello, world"))
	assert.Nil(t, err)
	dec, err := jane.Decrypt(enc)
	assert.Nil(t, err)
	assert.Equal(t, dec, []byte("hello, world"))

	jane2, err := New([]byte("password"), nil)
	assert.Nil(t, err)
	dec, err = jane2.Decrypt(enc)
	assert.NotNil(t, err)
	assert.NotEqual(t, dec, []byte("hello, world"))

	jane3, err := New([]byte("passwordwrong"), bob.Salt())
	assert.Nil(t, err)
	dec, err = jane3.Decrypt(enc)
	assert.NotNil(t, err)
	assert.NotEqual(t, dec, []byte("hello, world"))

}


func TestNoEncryption(t *testing.T) {
	bob, err := New(nil, nil)
	assert.Nil(t, err)
	jane, err := New(nil,nil)
	assert.Nil(t, err)
	enc, err := bob.Encrypt([]byte("hello, world"))
	assert.Nil(t, err)
	dec, err := jane.Decrypt(enc)
	assert.Nil(t, err)
	assert.Equal(t, dec, []byte("hello, world"))
	assert.Equal(t, enc, []byte("hello, world"))
	
}