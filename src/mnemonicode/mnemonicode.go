// From GitHub version/fork maintained by Stephen Paul Weber available at:
// https://github.com/singpolyma/mnemonicode
//
// Originally from:
// http://web.archive.org/web/20101031205747/http://www.tothink.com/mnemonic/

/*
 Copyright (c) 2000  Oren Tirosh <oren@hishome.net>

 Permission is hereby granted, free of charge, to any person obtaining a copy
 of this software and associated documentation files (the "Software"), to deal
 in the Software without restriction, including without limitation the rights
 to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 copies of the Software, and to permit persons to whom the Software is
 furnished to do so, subject to the following conditions:

 The above copyright notice and this permission notice shall be included in
 all copies or substantial portions of the Software.

 THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
 AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 THE SOFTWARE.
*/

package mnemonicode

const base = 1626

// WordsRequired returns the number of words required to encode input
// data of length bytes using mnomonic encoding.
//
// Every four bytes of input is encoded into three words. If there
// is an extra one or two bytes they get an extra one or two words
// respectively. If there is an extra three bytes, they will be encoded
// into three words with the last word being one of a small set of very
// short words (only needed to encode the last 3 bits).
func WordsRequired(length int) int {
	return ((length + 1) * 3) / 4
}

// EncodeWordList encodes src into mnemomic words which are appended to dst.
// The final wordlist is returned.
// There will be WordsRequired(len(src)) words appeneded.
func EncodeWordList(dst []string, src []byte) (result []string) {
	if n := len(dst) + WordsRequired(len(src)); cap(dst) < n {
		result = make([]string, len(dst), n)
		copy(result, dst)
	} else {
		result = dst
	}

	var x uint32
	for len(src) >= 4 {
		x = uint32(src[0])
		x |= uint32(src[1]) << 8
		x |= uint32(src[2]) << 16
		x |= uint32(src[3]) << 24
		src = src[4:]

		i0 := int(x % base)
		i1 := int(x/base) % base
		i2 := int(x/base/base) % base
		result = append(result, WordList[i0], WordList[i1], WordList[i2])
	}
	if len(src) > 0 {
		x = 0
		for i := len(src) - 1; i >= 0; i-- {
			x <<= 8
			x |= uint32(src[i])
		}
		i := int(x % base)
		result = append(result, WordList[i])
		if len(src) >= 2 {
			i = int(x/base) % base
			result = append(result, WordList[i])
		}
		if len(src) == 3 {
			i = base + int(x/base/base)%7
			result = append(result, WordList[i])
		}
	}

	return result
}
