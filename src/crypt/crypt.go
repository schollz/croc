package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/pbkdf2"
)

// New generates a new key based on a passphrase and salt
func New(passphrase []byte, usersalt []byte) (key []byte, salt []byte, err error) {
	if len(passphrase) < 1 {
		err = fmt.Errorf("need more than that for passphrase")
		return
	}
	if usersalt == nil {
		salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		if _, err := rand.Read(salt); err != nil {
			log.Fatalf("can't get random salt: %v", err)
		}
	} else {
		salt = usersalt
	}
	key = pbkdf2.Key(passphrase, salt, 100, 32, sha256.New)
	return
}

// Encrypt will encrypt using the pre-generated key
func Encrypt(plaintext []byte, key []byte) (encrypted []byte, err error) {
	// generate a random iv each time
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	ivBytes := make([]byte, 12)
	if _, err = rand.Read(ivBytes); err != nil {
		log.Fatalf("can't initialize crypto: %v", err)
	}
	b, err := aes.NewCipher(key)
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

// Decrypt using the pre-generated key
func Decrypt(encrypted []byte, key []byte) (plaintext []byte, err error) {
	if len(encrypted) < 13 {
		err = fmt.Errorf("incorrect passphrase")
		return
	}
	b, err := aes.NewCipher(key)
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

// NewArgon2 generates a new key based on a passphrase and salt
// using argon2
// https://pkg.go.dev/golang.org/x/crypto/argon2
func NewArgon2(passphrase []byte, usersalt []byte) (aead cipher.AEAD, salt []byte, err error) {
	if len(passphrase) < 1 {
		err = fmt.Errorf("need more than that for passphrase")
		return
	}
	if usersalt == nil {
		salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		if _, err = rand.Read(salt); err != nil {
			log.Fatalf("can't get random salt: %v", err)
		}
	} else {
		salt = usersalt
	}
	aead, err = chacha20poly1305.NewX(argon2.IDKey(passphrase, salt, 1, 64*1024, 4, 32))
	return
}

// EncryptChaCha will encrypt ChaCha20-Poly1305 using the pre-generated key
// https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305
func EncryptChaCha(plaintext []byte, aead cipher.AEAD) (encrypted []byte, err error) {
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}

	// Encrypt the message and append the ciphertext to the nonce.
	encrypted = aead.Seal(nonce, nonce, plaintext, nil)
	return
}

// DecryptChaCha will decrypt ChaCha20-Poly1305 using the pre-generated key
// https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305
func DecryptChaCha(encryptedMsg []byte, aead cipher.AEAD) (plaintext []byte, err error) {
	if len(encryptedMsg) < aead.NonceSize() {
		err = fmt.Errorf("ciphertext too short")
		return
	}

	// Split nonce and ciphertext.
	nonce, ciphertext := encryptedMsg[:aead.NonceSize()], encryptedMsg[aead.NonceSize():]

	// Decrypt the message and check it wasn't tampered with.
	plaintext, err = aead.Open(nil, nonce, ciphertext, nil)
	return
}
