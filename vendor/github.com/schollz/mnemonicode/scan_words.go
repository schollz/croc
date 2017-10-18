package mnemonicode

import (
	"unicode"
	"unicode/utf8"
)

// modified version of bufio.ScanWords from bufio/scan.go

// scanWords is a split function for a Scanner that returns
// each non-letter separated word of text, with surrounding
// non-leters deleted. It will never return an empty string.
// The definition of letter is set by unicode.IsLetter.
func scanWords(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Skip leading non-letters.
	start := 0
	for width := 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if unicode.IsLetter(r) {
			break
		}
	}
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	// Scan until non-letter, marking end of word.
	for width, i := 0, start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if !unicode.IsLetter(r) {
			return i + width, data[start:i], nil
		}
	}
	// If we're at EOF, we have a final, non-empty, non-terminated word. Return it.
	if atEOF && len(data) > start {
		return len(data), data[start:], nil
	}
	// Request more data.
	return 0, nil, nil
}
