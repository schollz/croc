package croc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"os"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/mars9/crypt"
	"github.com/schollz/mnemonicode"
	"golang.org/x/crypto/pbkdf2"
)

func init() {
	mathrand.Seed(time.Now().UTC().UnixNano())
}

func getRandomName() string {
	result := []string{}
	bs := make([]byte, 4)
	rand.Read(bs)
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}

func encrypt(plaintext []byte, passphrase string, dontencrypt ...bool) (encrypted []byte, salt string, iv string) {
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

func decrypt(data []byte, passphrase string, salt string, iv string, dontencrypt ...bool) (plaintext []byte, err error) {
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

func hash(data string) string {
	return hashBytes([]byte(data))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func encryptFile(inputFilename string, outputFilename string, password string) error {
	return cryptFile(inputFilename, outputFilename, password, true)
}

func decryptFile(inputFilename string, outputFilename string, password string) error {
	return cryptFile(inputFilename, outputFilename, password, false)
}

func cryptFile(inputFilename string, outputFilename string, password string, encrypt bool) error {
	in, err := os.Open(inputFilename)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(outputFilename)
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Sync(); err != nil {
			log.Error(err)
		}
		if err := out.Close(); err != nil {
			log.Error(err)
		}
	}()
	c := &crypt.Crypter{
		HashFunc: sha1.New,
		HashSize: sha1.Size,
		Key:      crypt.NewPbkdf2Key([]byte(password), 32),
	}
	if encrypt {
		if err := c.Encrypt(out, in); err != nil {
			return err
		}
	} else {
		if err := c.Decrypt(out, in); err != nil {
			return err
		}
	}
	return nil
}
