// For use with go-fuzz, "github.com/dvyukov/go-fuzz"
//
// +build gofuzz

package mnemonicode

import (
	"bytes"
	"fmt"

	"golang.org/x/text/transform"
)

var (
	tenc    = NewEncodeTransformer(nil)
	tdec    = NewDecodeTransformer()
	tencdec = transform.Chain(tenc, tdec)
)

//go:generate go-fuzz-build bitbucket.org/dchapes/mnemonicode
// Then:
//	go-fuzz -bin=mnemonicode-fuzz.zip -workdir=fuzz

// Fuzz is for use with go-fuzz, "github.com/dvyukov/go-fuzz"
func Fuzz(data []byte) int {
	words := EncodeWordList(nil, data)
	if len(words) != WordsRequired(len(data)) {
		panic("bad WordsRequired result")
	}
	data2, err := DecodeWordList(nil, words)
	if err != nil {
		fmt.Println("words:", words)
		panic(err)
	}
	if !bytes.Equal(data, data2) {
		fmt.Println("words:", words)
		panic("data != data2")
	}

	data3, _, err := transform.Bytes(tencdec, data)
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(data, data3) {
		fmt.Println("words:", words)
		panic("data != data3")
	}

	if len(data) == 0 {
		return 0
	}
	return 1
}

//go:generate go-fuzz-build -func Fuzz2 -o mnemonicode-fuzz2.zip bitbucket.org/dchapes/mnemonicode
// Then:
//	go-fuzz -bin=mnemonicode-fuzz2.zip -workdir=fuzz2

// Fuzz2 is another fuzz tester, this time with words as input rather than binary data.
func Fuzz2(data []byte) int {
	_, _, err := transform.Bytes(tdec, data)
	if err != nil {
		if _, ok := err.(WordError); !ok {
			return 0
		}
		fmt.Println("Unexpected error")
		panic(err)
	}
	return 1
}
