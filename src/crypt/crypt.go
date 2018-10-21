package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// Encryption stores the data
type Encryption struct {
	Encrypted []byte `json:"e"`
	Salt      []byte `json:"s"`
	IV        []byte `json:"i"`
}

func (e Encryption) Bytes() []byte {
	return []byte(base64.StdEncoding.EncodeToString(e.Encrypted) + "-" + base64.StdEncoding.EncodeToString(e.Salt) + "-" + base64.StdEncoding.EncodeToString(e.IV))
}

func FromBytes(b []byte) (enc Encryption, err error) {
	enc = Encryption{}
	items := strings.Split(string(b), "-")
	if len(items) != 3 {
		err = errors.New("not valid")
		return
	}
	enc.Encrypted, err = base64.StdEncoding.DecodeString(items[0])
	if err != nil {
		return
	}
	enc.Salt, err = base64.StdEncoding.DecodeString(items[1])
	if err != nil {
		return
	}
	enc.IV, err = base64.StdEncoding.DecodeString(items[2])
	return
}

// Encrypt will generate an encryption
func Encrypt(plaintext []byte, passphrase []byte, dontencrypt ...bool) Encryption {
	if len(dontencrypt) > 0 && dontencrypt[0] {
		return Encryption{
			Encrypted: plaintext,
			Salt:      []byte("salt"),
			IV:        []byte("iv"),
		}
	}
	key, saltBytes := deriveKey(passphrase, nil)
	ivBytes := make([]byte, 12)
	// http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf
	// Section 8.2
	rand.Read(ivBytes)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	encrypted := aesgcm.Seal(nil, ivBytes, plaintext, nil)
	return Encryption{
		Encrypted: encrypted,
		Salt:      saltBytes,
		IV:        ivBytes,
	}
}

// Decrypt an encryption
func (e Encryption) Decrypt(passphrase []byte, dontencrypt ...bool) (plaintext []byte, err error) {
	if len(dontencrypt) > 0 && dontencrypt[0] {
		return e.Encrypted, nil
	}
	key, _ := deriveKey(passphrase, e.Salt)
	b, _ := aes.NewCipher(key)
	aesgcm, _ := cipher.NewGCM(b)
	plaintext, err = aesgcm.Open(nil, e.IV, e.Encrypted, nil)
	return
}

func deriveKey(passphrase []byte, salt []byte) ([]byte, []byte) {
	if salt == nil {
		salt = make([]byte, 8)
		// http://www.ietf.org/rfc/rfc2898.txt
		// Salt.
		rand.Read(salt)
	}
	return pbkdf2.Key([]byte(passphrase), salt, 100, 32, sha256.New), salt
}
