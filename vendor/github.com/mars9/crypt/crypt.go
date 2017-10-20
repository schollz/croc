// Package crypt provides password-based encryption and decryption of
// data streams.
package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"hash"
	"io"
)

const (
	blockSize = aes.BlockSize // AES block size
	version   = 1
)

// Crypter encrypt/decrypts with AES (Rijndael) in cipher block counter
// mode (CTR) and authenticate with HMAC-SHA.
type Crypter struct {
	HashFunc func() hash.Hash
	HashSize int
	Key      Key
	BufSize  int
}

func (c *Crypter) encHeader(salt, iv, hmacKey []byte) []byte {
	keySize := c.Key.Size()
	headerSize := 1 + keySize + blockSize + c.HashSize

	b := make([]byte, headerSize)
	b[0] = version
	copy(b[1:1+keySize], salt)
	copy(b[1+keySize:1+keySize+blockSize], iv)

	mac := hmac.New(c.HashFunc, hmacKey)
	mac.Write(b[:1+keySize+blockSize])
	copy(b[1+keySize+blockSize:], mac.Sum(nil))
	return b
}

func (c *Crypter) bufSize() int {
	if c.BufSize == 0 {
		return 2 * 1024 * 1024
	}
	return c.BufSize
}

func (c *Crypter) decHeader(b []byte) ([]byte, []byte, error) {
	if b[0] != version {
		return nil, nil, errors.New("malformed encrypted packet")
	}

	keySize := c.Key.Size()
	salt := b[1 : 1+keySize]
	iv := b[1+keySize : 1+keySize+blockSize]
	return salt, iv, nil
}

// Encrypt encrypts from src until either EOF is reached on src or an
// error occurs. A successful Encrypt returns err == nil, not err == EOF.
func (c *Crypter) Encrypt(dst io.Writer, src io.Reader) (err error) {
	salt := make([]byte, c.Key.Size())
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	iv := make([]byte, blockSize)
	if _, err := rand.Read(iv); err != nil {
		return err
	}

	aesKey, hmacKey := c.Key.Derive(salt)
	header := c.encHeader(salt, iv, hmacKey)
	if _, err := dst.Write(header); err != nil {
		return err
	}
	mac := hmac.New(c.HashFunc, hmacKey)
	mac.Write(header)

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return err
	}

	stream := cipher.NewCTR(block, iv)

	buf := make([]byte, c.bufSize())
	n := 0
	for {
		n, err = src.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		mac.Write(buf[:n])
		stream.XORKeyStream(buf[:n], buf[:n])
		if _, err = dst.Write(buf[:n]); err != nil {
			return err
		}
		if _, err = dst.Write(mac.Sum(nil)); err != nil {
			return err
		}
	}
	return nil
}

// Decrypt decrypts from src until either EOF is reached on src or an
// error occurs. A successful Decrypt returns err == nil, not err == EOF.
func (c *Crypter) Decrypt(dst io.Writer, src io.Reader) (err error) {
	keySize := c.Key.Size()
	headerSize := 1 + keySize + blockSize + c.HashSize

	header := make([]byte, headerSize)
	if _, err = src.Read(header); err != nil {
		return err
	}

	salt, iv, err := c.decHeader(header)
	if err != nil {
		return err
	}
	aesKey, hmacKey := c.Key.Derive(salt)

	mac := hmac.New(c.HashFunc, hmacKey)
	mac.Write(header[:1+keySize+blockSize])

	if !bytes.Equal(header[1+keySize+blockSize:], mac.Sum(nil)) {
		return errors.New("cannot authenticate header")
	}
	mac.Write(header[1+keySize+blockSize:])

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return err
	}

	stream := cipher.NewCTR(block, iv)
	buf := make([]byte, c.bufSize()+c.HashSize)
	n := 0
	for {
		n, err = src.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		stream.XORKeyStream(buf[:n-c.HashSize], buf[:n-c.HashSize])
		mac.Write(buf[:n-c.HashSize])
		if !bytes.Equal(buf[n-c.HashSize:n], mac.Sum(nil)) {
			return errors.New("cannot authenticate packet")
		}
		if _, err = dst.Write(buf[:n-c.HashSize]); err != nil {
			return err
		}
	}
	return nil
}
