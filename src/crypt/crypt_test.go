package crypt

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func BenchmarkEncrypt(b *testing.B) {
	bob, _, _ := New([]byte("password"), nil)
	for i := 0; i < b.N; i++ {
		Encrypt([]byte("hello, world"), bob)
	}
}

func BenchmarkTransferChunkEncryption(b *testing.B) {
	key, _, err := New([]byte("password"), nil)
	if err != nil {
		b.Fatal(err)
	}
	aead, err := NewAESGCM(key)
	if err != nil {
		b.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("x"), 32*1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(chunk)))
	b.Run("legacy-new-aead-per-chunk", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Encrypt(chunk, key); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reused-aead", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := EncryptAEAD(chunk, aead); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reused-aead-and-buffer", func(b *testing.B) {
		buffer := make([]byte, 0, len(chunk)+aead.NonceSize()+aead.Overhead())
		for i := 0; i < b.N; i++ {
			var err error
			buffer, err = EncryptAEADTo(buffer, chunk, aead)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
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

func BenchmarkNewPbkdf2(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New([]byte("password"), nil)
	}
}

func BenchmarkNewArgon2(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewArgon2([]byte("password"), nil)
	}
}

func BenchmarkEncryptChaCha(b *testing.B) {
	bob, _, _ := NewArgon2([]byte("password"), nil)
	for i := 0; i < b.N; i++ {
		EncryptChaCha([]byte("hello, world"), bob)
	}
}

func BenchmarkDecryptChaCha(b *testing.B) {
	key, _, _ := NewArgon2([]byte("password"), nil)
	msg := []byte("hello, world")
	enc, _ := EncryptChaCha(msg, key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecryptChaCha(enc, key)
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
	key2, _, _ := New([]byte("password"), salt)
	dec, err = Decrypt(enc, key2)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, _ = New([]byte("wrong password"), salt)
	dec, err = Decrypt(enc, key2)
	assert.NotNil(t, err)
	assert.NotEqual(t, msg, dec)

	// error with no password
	_, err = Decrypt([]byte(""), key)
	assert.NotNil(t, err)

	// error with small password
	_, _, err = New([]byte(""), nil)
	assert.NotNil(t, err)
}

func TestReusableAEADPreservesWireFormat(t *testing.T) {
	key, _, err := New([]byte("password"), nil)
	assert.NoError(t, err)
	aead, err := NewAESGCM(key)
	assert.NoError(t, err)
	msg := []byte("wire-compatible transfer chunk")

	legacyCiphertext, err := Encrypt(msg, key)
	assert.NoError(t, err)
	decoded, err := DecryptAEAD(legacyCiphertext, aead)
	assert.NoError(t, err)
	assert.Equal(t, msg, decoded)

	reusedCiphertext, err := EncryptAEAD(msg, aead)
	assert.NoError(t, err)
	decoded, err = Decrypt(reusedCiphertext, key)
	assert.NoError(t, err)
	assert.Equal(t, msg, decoded)

	inPlaceCiphertext := append([]byte(nil), reusedCiphertext...)
	decoded, err = DecryptAEADInPlace(inPlaceCiphertext, aead)
	assert.NoError(t, err)
	assert.Equal(t, msg, decoded)
}

func TestEncryptionChaCha(t *testing.T) {
	key, salt, err := NewArgon2([]byte("password"), nil)
	fmt.Printf("key: %x\n", key)
	assert.Nil(t, err)
	msg := []byte("hello, world")
	enc, err := EncryptChaCha(msg, key)
	assert.Nil(t, err)
	dec, err := DecryptChaCha(enc, key)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, _ := NewArgon2([]byte("password"), salt)
	dec, err = DecryptChaCha(enc, key2)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, _ = NewArgon2([]byte("wrong password"), salt)
	dec, err = DecryptChaCha(enc, key2)
	assert.NotNil(t, err)
	assert.NotEqual(t, msg, dec)

	// error with no password
	_, err = DecryptChaCha([]byte(""), key)
	assert.NotNil(t, err)

	// error with small password
	_, _, err = NewArgon2([]byte(""), nil)
	assert.NotNil(t, err)
}
