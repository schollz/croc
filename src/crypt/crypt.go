package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"strings"

	"filippo.io/age"
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
	if _, err := rand.Read(ivBytes); err != nil {
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

// GenerateIdentityAndPassword will generate a file with a public age identity and password
func GenerateIdentityAndPassword(fname string) (err error) {
	keyPublic, keyPrivate, err := NewAge()
	if err != nil {
		return
	}
	password, err := GenerateRandomString(6)
	if err != nil {
		return
	}
	err = ioutil.WriteFile(fname, []byte(fmt.Sprintf("%s\n%s\n%s", keyPrivate, keyPublic, password)), 0644)
	return
}

// LoadIdentityAndPassword will load a file with a public age identity and password
func LoadIdentityAndPassword(fname string) (keyPrivate string, keyPublic string, password string, err error) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return
	}
	foo := strings.Fields(string(b))
	if len(foo) < 3 {
		err = fmt.Errorf("malformed file")
		return
	}
	keyPrivate = foo[0]
	keyPublic = foo[1]
	password = foo[2]

	_, err = age.ParseX25519Identity(keyPrivate)
	if err != nil {
		return
	}
	_, err = age.ParseX25519Recipient(keyPublic)
	if err != nil {
		return
	}
	return
}

// GenerateRandomString returns a securely generated random string.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomString(n int) (string, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		ret[i] = letters[num.Int64()]
	}

	return string(ret), nil
}

func NewAge() (pubkey string, privkey string, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return
	}
	pubkey = identity.Recipient().String()
	privkey = identity.String()
	return
}

func EncryptAge(data []byte, pubkey string) (encrypted []byte, err error) {
	recipient, err := age.ParseX25519Recipient(pubkey)
	if err != nil {
		return
	}

	out := &bytes.Buffer{}
	w, err := age.Encrypt(out, recipient)
	if err != nil {
		return
	}
	_, err = w.Write(data)
	if err != nil {
		return
	}
	err = w.Close()
	if err != nil {
		return
	}
	encrypted = out.Bytes()
	return
}

func DecryptAge(encrypted []byte, privkey string) (data []byte, err error) {
	identity, err := age.ParseX25519Identity(privkey)
	if err != nil {
		return
	}

	r, err := age.Decrypt(bytes.NewReader(encrypted), identity)
	if err != nil {
		return
	}

	out := &bytes.Buffer{}
	_, err = io.Copy(out, r)
	if err != nil {
		return
	}

	data = out.Bytes()
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
		if _, err := rand.Read(salt); err != nil {
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

// DecryptChaCha will encrypt ChaCha20-Poly1305 using the pre-generated key
// https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305
func DecryptChaCha(encryptedMsg []byte, aead cipher.AEAD) (encrypted []byte, err error) {
	if len(encryptedMsg) < aead.NonceSize() {
		err = fmt.Errorf("ciphertext too short")
		return
	}

	// Split nonce and ciphertext.
	nonce, ciphertext := encryptedMsg[:aead.NonceSize()], encryptedMsg[aead.NonceSize():]

	// Decrypt the message and check it wasn't tampered with.
	encrypted, err = aead.Open(nil, nonce, ciphertext, nil)
	return
}
