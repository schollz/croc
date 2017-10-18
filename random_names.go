package main

import (
	"encoding/binary"
	"math/rand"
	"strings"
	"time"

	"github.com/schollz/mnemonicode"
)

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
}

func GetRandomName() string {
	result := []string{}
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, rand.Uint32())
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}
