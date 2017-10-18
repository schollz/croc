package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	mathrand "math/rand"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/schollz/mnemonicode"
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

func Encrypt(plaintext []byte, key string) (ciphertext []byte, err error) {
	newKey := ""
	for i := 0; i < 32; i++ {
		if i < len(key) {
			newKey += string(key[i])
		} else {
			newKey += ":"
		}
	}
	block, err := aes.NewCipher([]byte(newKey))
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func Decrypt(ciphertext []byte, key string) (plaintext []byte, err error) {
	newKey := ""
	for i := 0; i < 32; i++ {
		if i < len(key) {
			newKey += string(key[i])
		} else {
			newKey += ":"
		}
	}
	block, err := aes.NewCipher([]byte(newKey))
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("malformed ciphertext")
	}

	return gcm.Open(nil,
		ciphertext[:gcm.NonceSize()],
		ciphertext[gcm.NonceSize():],
		nil,
	)
}

func Hash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", sum)
}
