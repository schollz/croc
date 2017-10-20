package crypt

import (
	"crypto/sha1"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

// Key defines the key derivation function interface.
type Key interface {
	// Derive returns the AES key and HMAC-SHA key, for the given password,
	// salt combination.
	Derive(salt []byte) (aesKey, hmacKey []byte)

	// Size returns the key-size. Key-size should either 16, 24, or 32 to
	// select AES-128, AES-192, or AES-256.
	Size() int

	// Reset resets/flushes the key.
	Reset()
}

type pbkdf2Key struct {
	password []byte
	size     int
}

// NewPbkdf2Key returns the key derivation function PBKDF2 as defined in
// RFC 2898.
func NewPbkdf2Key(password []byte, size int) Key {
	return pbkdf2Key{password: password, size: size}
}

func (k pbkdf2Key) Derive(salt []byte) (aesKey, hmacKey []byte) {
	key := pbkdf2.Key(k.password, salt, 4096, 2*k.size, sha1.New)
	aesKey = key[:k.size]
	hmacKey = key[k.size:]
	return aesKey, hmacKey
}

func (k pbkdf2Key) Size() int { return k.size }

func (k pbkdf2Key) Reset() {
	for i := range k.password {
		k.password[i] = 0
	}
}

type scryptKey struct {
	password []byte
	size     int
}

// NewScryptKey returns the scrypt key derivation function as defined in
// Colin Percival's paper "Stronger Key Derivation via Sequential
// Memory-Hard Functions".
func NewScryptKey(password []byte, size int) Key {
	return scryptKey{password: password, size: size}
}

func (k scryptKey) Derive(salt []byte) (aesKey, hmacKey []byte) {
	key, err := scrypt.Key(k.password, salt, 16384, 8, 1, 2*k.size)
	if err != nil {
		panic(err)
	}

	aesKey = key[:k.size]
	hmacKey = key[k.size:]
	return aesKey, hmacKey
}

func (k scryptKey) Size() int { return k.size }

func (k scryptKey) Reset() {
	for i := range k.password {
		k.password[i] = 0
	}
}
