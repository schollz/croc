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
	aesgcm, err := NewAESGCM(key)
	if err != nil {
		return nil, err
	}
	return EncryptAEAD(plaintext, aesgcm)
}

// NewAESGCM constructs reusable AES-GCM state for a transfer connection.
func NewAESGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// EncryptAEAD encrypts plaintext with reusable AEAD state. Its wire format is
// identical to Encrypt: nonce followed by ciphertext and authentication tag.
func EncryptAEAD(plaintext []byte, aead cipher.AEAD) (encrypted []byte, err error) {
	return EncryptAEADTo(nil, plaintext, aead)
}

// EncryptAEADTo encrypts into dst when it has sufficient capacity, allowing
// transfer loops to reuse their ciphertext buffer across chunks.
func EncryptAEADTo(dst, plaintext []byte, aead cipher.AEAD) (encrypted []byte, err error) {
	// generate a random iv each time
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	required := aead.NonceSize() + len(plaintext) + aead.Overhead()
	if cap(dst) < required {
		dst = make([]byte, aead.NonceSize(), required)
	} else {
		dst = dst[:aead.NonceSize()]
	}
	ivBytes := dst
	if _, err = rand.Read(ivBytes); err != nil {
		return nil, fmt.Errorf("can't initialize crypto: %w", err)
	}
	encrypted = aead.Seal(ivBytes, ivBytes, plaintext, nil)
	return
}

// Decrypt using the pre-generated key
func Decrypt(encrypted []byte, key []byte) (plaintext []byte, err error) {
	aesgcm, err := NewAESGCM(key)
	if err != nil {
		return nil, err
	}
	return DecryptAEAD(encrypted, aesgcm)
}

// DecryptAEAD decrypts data produced by Encrypt or EncryptAEAD using reusable
// AEAD state.
func DecryptAEAD(encrypted []byte, aead cipher.AEAD) (plaintext []byte, err error) {
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		err = fmt.Errorf("incorrect passphrase")
		return
	}
	plaintext, err = aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], nil)
	return
}

// DecryptAEADInPlace authenticates and decrypts into the ciphertext portion of
// encrypted, avoiding a payload-sized allocation in transfer receive loops.
func DecryptAEADInPlace(encrypted []byte, aead cipher.AEAD) (plaintext []byte, err error) {
	if len(encrypted) < aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("incorrect passphrase")
	}
	nonceSize := aead.NonceSize()
	nonce := encrypted[:nonceSize]
	ciphertext := encrypted[nonceSize:]
	return aead.Open(ciphertext[:0], nonce, ciphertext, nil)
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
		return nil, err
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
