package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"

	"golang.org/x/crypto/pbkdf2"
)

type Encryption struct {
	key        []byte
	passphrase []byte
	salt       []byte
}

// New generates a new Encryption, using the supplied passphrase and
// an optional supplied salt.
// Passing nil passphrase will not use decryption.
func New(passphrase []byte, salt []byte) (e Encryption, err error) {
	if passphrase == nil {
		e = Encryption{nil, nil, nil}
		return
	}
	e.passphrase = passphrase
	if salt == nil {
		e.salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		rand.Read(e.salt)
	} else {
		e.salt = salt
	}
	e.key = pbkdf2.Key([]byte(passphrase), e.salt, 100, 32, sha256.New)
	return
}

func (e Encryption) Salt() []byte {
	return e.salt
}

// Encrypt will generate an Encryption, prefixed with the IV
func (e Encryption) Encrypt(plaintext []byte) (encrypted []byte, err error) {
	if e.passphrase == nil {
		encrypted = plaintext
		return
	}
	// generate a random iv each time
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	ivBytes := make([]byte, 12)
	rand.Read(ivBytes)
	b, err := aes.NewCipher(e.key)
	if err != nil {
		return
	}
	aesgcm, err := cipher.NewGCM(b)
	if err != nil {
		return
	}
	encrypted = aesgcm.Seal(nil, ivBytes, plaintext, nil)
	encrypted = append(ivBytes, encrypted...)
	return
}

// Decrypt an Encryption
func (e Encryption) Decrypt(encrypted []byte) (plaintext []byte, err error) {
	if e.passphrase == nil {
		plaintext = encrypted
		return
	}
	b, err := aes.NewCipher(e.key)
	if err != nil {
		return
	}
	aesgcm, err := cipher.NewGCM(b)
	if err != nil {
		return
	}
	plaintext, err = aesgcm.Open(nil, encrypted[:12], encrypted[12:], nil)
	return
}
