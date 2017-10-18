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

func Encrypt(plaintext []byte, passphrase string) ([]byte, string, string) {
	key, salt := deriveKey(passphrase, nil)
	iv := make([]byte, 12)
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	rand.Read(iv)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	data := aesgcm.Seal(nil, iv, plaintext, nil)
	return data, hex.EncodeToString(salt), hex.EncodeToString(iv)
}

func Decrypt(data []byte, passphrase string, salt string, iv string) (plaintext []byte, err error) {
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
