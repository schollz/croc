package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"

	"github.com/schollz/mnemonicode"
	"golang.org/x/crypto/pbkdf2"
)

func init() {
	mathrand.Seed(time.Now().UTC().UnixNano())
}

func GetRandomName() string {
	result := []string{}
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, mathrand.Uint32())
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}

func Encrypt(plaintext []byte, passphrase string, dontencrypt ...bool) (encrypted []byte, salt string, iv string) {
	if len(dontencrypt) > 0 && dontencrypt[0] {
		return plaintext, "salt", "iv"
	}
	key, saltBytes := deriveKey(passphrase, nil)
	ivBytes := make([]byte, 12)
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	rand.Read(ivBytes)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	encrypted = aesgcm.Seal(nil, ivBytes, plaintext, nil)
	salt = hex.EncodeToString(saltBytes)
	iv = hex.EncodeToString(ivBytes)
	return
}

func Decrypt(data []byte, passphrase string, salt string, iv string, dontencrypt ...bool) (plaintext []byte, err error) {
	if len(dontencrypt) > 0 && dontencrypt[0] {
		return data, nil
	}
	saltBytes, _ := hex.DecodeString(salt)
	ivBytes, _ := hex.DecodeString(iv)
	key, _ := deriveKey(passphrase, saltBytes)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	plaintext, err = aesgcm.Open(nil, ivBytes, data, nil)
	return
}

func deriveKey(passphrase string, salt []byte) ([]byte, []byte) {
	if salt == nil {
		salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		rand.Read(salt)
	}
	return pbkdf2.Key([]byte(passphrase), salt, 1000, 32, sha256.New), salt
}

func Hash(data string) string {
	return HashBytes([]byte(data))
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
