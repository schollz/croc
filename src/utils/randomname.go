package utils

import (
	"crypto/rand"
	"strings"

	"github.com/schollz/mnemonicode"
)

func GetRandomName() string {
	result := []string{}
	bs := make([]byte, 4)
	rand.Read(bs)
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}
