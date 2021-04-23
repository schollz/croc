package crypt

import (
	"fmt"
	"os"
	"testing"

	"github.com/schollz/croc/v9/src/utils"
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
	key2, _, err := NewArgon2([]byte("password"), salt)
	dec, err = DecryptChaCha(enc, key2)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)

	// check reusing the salt
	key2, _, err = NewArgon2([]byte("wrong password"), salt)
	dec, err = DecryptChaCha(enc, key2)
	assert.NotNil(t, err)
	assert.NotEqual(t, msg, dec)

	// error with no password
	dec, err = DecryptChaCha([]byte(""), key)
	assert.NotNil(t, err)

	// error with small password
	_, _, err = NewArgon2([]byte(""), nil)
	assert.NotNil(t, err)
}

func TestEncryptionAge(t *testing.T) {
	pub, priv, err := NewAge()
	fmt.Printf("key: %s\n", pub)
	assert.Nil(t, err)
	msg := []byte("hello, world")
	enc, err := EncryptAge(msg, pub)
	assert.Nil(t, err)
	dec, err := DecryptAge(enc, priv)
	assert.Nil(t, err)
	assert.Equal(t, msg, dec)
}

func TestGenerate(t *testing.T) {
	err := GenerateIdentityAndPassword("test")
	assert.Nil(t, err)
	assert.True(t, utils.Exists("test"))
	keyPrivate, keyPublic, password, err := LoadIdentityAndPassword("test")
	assert.Nil(t, err)
	fmt.Println(keyPrivate)
	fmt.Println(keyPublic)
	fmt.Println(password)
	_, _, _, err = LoadIdentityAndPassword("crypt.go")
	assert.NotNil(t, err)
	_, _, _, err = LoadIdentityAndPassword("doesntexist")
	assert.NotNil(t, err)
	os.Remove("test")
}
