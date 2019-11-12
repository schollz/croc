package box

import (
	"encoding/base64"

	jsoniter "github.com/json-iterator/go"
	"github.com/schollz/croc/v7/src/compress"
	"github.com/schollz/croc/v7/src/crypt"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// Bundle will marshal, encrypt, and compress some data into a base64 string
func Bundle(payload interface{}, key []byte) (bundled string, err error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return
	}
	p = compress.Compress(p)
	if key != nil {
		p, err = crypt.Encrypt(p, key)
		if err != nil {
			return
		}
	}
	// TODO: use base-122 encoding instead? https://github.com/kevinAlbs/Base122
	bundled = base64.StdEncoding.EncodeToString(p)
	return
}

// Unbundle will decode, decrypt, and decompress the payload into the interface
func Unbundle(bundled string, key []byte, payload interface{}) (err error) {
	b, err := base64.StdEncoding.DecodeString(bundled)
	if err != nil {
		return
	}
	if key != nil {
		b, err = crypt.Decrypt(b, key)
		if err != nil {
			return
		}
	}
	b = compress.Decompress(b)
	err = json.Unmarshal(b, &payload)
	return
}
