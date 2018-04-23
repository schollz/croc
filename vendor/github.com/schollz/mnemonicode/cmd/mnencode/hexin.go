package main

import (
	"encoding/hex"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

type hexinput bool

func (h *hexinput) Reset() {
	*h = false
}

func (h *hexinput) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	for r, sz := rune(0), 0; len(src) > 0; src = src[sz:] {
		if r = rune(src[0]); r < utf8.RuneSelf {
			sz = 1
		} else {
			r, sz = utf8.DecodeRune(src)
			if sz == 1 {
				// Invalid rune.
				if !atEOF && !utf8.FullRune(src) {
					err = transform.ErrShortSrc
					break
				}
				// Just ignore it
				nSrc++
				continue
			}
		}
		if unicode.IsSpace(r) {
			nSrc += sz
			continue
		}
		if sz > 1 {
			err = hex.InvalidByteError(src[0]) // XXX
			break
		}
		if len(src) < 2 {
			err = transform.ErrShortSrc
			break
		}
		if nDst+1 > len(dst) {
			err = transform.ErrShortDst
			break
		}

		sz = 2
		nSrc += 2
		if !*h {
			*h = true
			if r == '0' && (src[1] == 'x' || src[1] == 'X') {
				continue
			}
		}

		if _, err = hex.Decode(dst[nDst:], src[:2]); err != nil {
			break
		}
		nDst++
	}
	return
}
