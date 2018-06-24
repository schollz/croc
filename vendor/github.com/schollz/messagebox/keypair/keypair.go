package keypair

import (
	"bytes"
	crypto_rand "crypto/rand"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	math_rand "math/rand"

	base65536 "github.com/Nightbug/go-base65536"
	"github.com/mr-tron/base58/base58"
	"golang.org/x/crypto/nacl/box"
)

var baseType = "base58"

func SetBase(s string) (err error) {
	if s == "base58" {
		baseType = s
	} else if s == "base65536" {
		baseType = s
	} else {
		err = errors.New("no such base")
	}
	return
}

type KeyPair struct {
	Public  string `json:"public"`
	Private string `json:"private,omitempty"`
	private *[32]byte
	public  *[32]byte
}

func (kp KeyPair) String() string {
	b, _ := json.Marshal(kp)
	return string(b)
}

// New will generate a new key pair, or reload a keypair
// from a public key or a public-private key pair.
func New(kpLoad ...KeyPair) (kp KeyPair, err error) {
	kp = KeyPair{}
	if len(kpLoad) > 0 {
		kp.Public = kpLoad[0].Public
		kp.Private = kpLoad[0].Private
	} else {
		kp.Public, kp.Private, err = generateKeyPair()
		if err != nil {
			return
		}
	}
	kp.public, err = keyToBytes(kp.Public)
	if err != nil {
		return
	}
	if len(kp.Private) > 0 {
		kp.private, err = keyToBytes(kp.Private)
		if err != nil {
			return
		}
	}
	return
}

func generateKeyPair() (publicKey, privateKey string, err error) {
	publicKeyBytes, privateKeyBytes, err := box.GenerateKey(crypto_rand.Reader)
	if err != nil {
		return
	}

	if baseType == "base58" {
		publicKey = base58.FastBase58Encoding(publicKeyBytes[:])
		privateKey = base58.FastBase58Encoding(privateKeyBytes[:])
	} else if baseType == "base65536" {
		publicKey = base65536.Marshal(publicKeyBytes[:])
		privateKey = base65536.Marshal(privateKeyBytes[:])
	}
	return
}

func generateDeterministicKey(seedBytes []byte) (publicKey, privateKey string) {
	h := fnv.New32a()
	h.Write(seedBytes)
	math_rand.Seed(int64(h.Sum32()))
	b := make([]byte, 512)
	math_rand.Read(b)
	reader := bytes.NewReader(b)
	publicKeyBytes, privateKeyBytes, err := box.GenerateKey(reader)
	if err != nil {
		panic(err)
	}

	if baseType == "base58" {
		publicKey = base58.FastBase58Encoding(publicKeyBytes[:])
		privateKey = base58.FastBase58Encoding(privateKeyBytes[:])
	} else if baseType == "base65536" {
		publicKey = base65536.Marshal(publicKeyBytes[:])
		privateKey = base65536.Marshal(privateKeyBytes[:])
	}
	return
}

// NewDeterministic will return a deterministic key
func NewDeterministic(passphrase string) (kp KeyPair, err error) {
	pub, priv := generateDeterministicKey([]byte(passphrase))
	kp, err = New(KeyPair{Public: pub, Private: priv})
	return
}

func keyToBytes(s string) (key *[32]byte, err error) {
	var keyBytes []byte
	if baseType == "base58" {
		keyBytes, err = base58.FastBase58Decoding(s)
	} else if baseType == "base65536" {
		err = base65536.Unmarshal([]byte(s), &keyBytes)
	}
	if err != nil {
		return
	}

	key = new([32]byte)
	copy(key[:], keyBytes[:32])
	return
}

// Encrypt a message for a recipient
func (kp KeyPair) Encrypt(msg []byte, recipientPublicKey string) (encrypted []byte, err error) {
	recipient, err := New(KeyPair{Public: recipientPublicKey})
	if err != nil {
		return
	}
	encrypted, err = encryptWithKeyPair(msg, kp.private, recipient.public)
	return
}

// Decrypt a message
func (kp KeyPair) Decrypt(encrypted []byte, senderPublicKey string) (msg []byte, err error) {
	sender, err := New(KeyPair{Public: senderPublicKey})
	if err != nil {
		return
	}
	msg, err = decryptWithKeyPair(encrypted, sender.public, kp.private)
	return
}

func encryptWithKeyPair(msg []byte, senderPrivateKey, recipientPublicKey *[32]byte) (encrypted []byte, err error) {
	// You must use a different nonce for each message you encrypt with the
	// same key. Since the nonce here is 192 bits long, a random value
	// provides a sufficiently small probability of repeats.
	var nonce [24]byte
	if _, err = io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		return
	}
	// This encrypts msg and appends the result to the nonce.
	encrypted = box.Seal(nonce[:], msg, &nonce, recipientPublicKey, senderPrivateKey)
	return
}

func decryptWithKeyPair(enc []byte, senderPublicKey, recipientPrivateKey *[32]byte) (decrypted []byte, err error) {
	// The recipient can decrypt the message using their private key and the
	// sender's public key. When you decrypt, you must use the same nonce you
	// used to encrypt the message. One way to achieve this is to store the
	// nonce alongside the encrypted message. Above, we stored the nonce in the
	// first 24 bytes of the encrypted text.
	var decryptNonce [24]byte
	copy(decryptNonce[:], enc[:24])
	var ok bool
	decrypted, ok = box.Open(nil, enc[24:], &decryptNonce, senderPublicKey, recipientPrivateKey)
	if !ok {
		err = errors.New("keypair decryption failed")
	}
	return
}

// sliceForAppend takes a slice and a requested number of bytes. It returns a
// slice with the contents of the given slice followed by that many bytes and a
// second slice that aliases into it and contains only the extra bytes. If the
// original slice has sufficient capacity then no allocation is performed.
func sliceForAppend(in []byte, n int) (head, tail []byte) {
	if total := len(in) + n; cap(in) >= total {
		head = in[:total]
	} else {
		head = make([]byte, total)
		copy(head, in)
	}
	tail = head[len(in):]
	return
}
