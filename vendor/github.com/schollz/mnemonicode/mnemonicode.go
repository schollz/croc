// Package mnemonicode …
package mnemonicode

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/transform"
)

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

// A Config structure contains options for mneomonic encoding.
//
// {PREFIX}word{wsep}word{gsep}word{wsep}word{SUFFIX}
type Config struct {
	LinePrefix     string
	LineSuffix     string
	WordSeparator  string
	GroupSeparator string
	WordsPerGroup  uint
	GroupsPerLine  uint
	WordPadding    rune
}

var defaultConfig = Config{
	LinePrefix:     "",
	LineSuffix:     "\n",
	WordSeparator:  " ",
	GroupSeparator: " - ",
	WordsPerGroup:  3,
	GroupsPerLine:  3,
	WordPadding:    ' ',
}

// NewDefaultConfig returns a newly allocated Config initialised with default values.
func NewDefaultConfig() *Config {
	r := new(Config)
	*r = defaultConfig
	return r
}

// NewEncodeReader returns a new io.Reader that will return a
// formatted list of mnemonic words representing the bytes in r.
//
// The configuration of the word formatting is controlled
// by c, which can be nil for default formatting.
func NewEncodeReader(r io.Reader, c *Config) io.Reader {
	t := NewEncodeTransformer(c)
	return transform.NewReader(r, t)
}

// NewEncoder returns a new io.WriteCloser that will write a formatted
// list of mnemonic words representing the bytes written to w. The user
// needs to call Close to flush unwritten bytes that may be buffered.
//
// The configuration of the word formatting is controlled
// by c, which can be nil for default formatting.
func NewEncoder(w io.Writer, c *Config) io.WriteCloser {
	t := NewEncodeTransformer(c)
	return transform.NewWriter(w, t)
}

// NewEncodeTransformer returns a new transformer
// that encodes bytes into mnemonic words.
//
// The configuration of the word formatting is controlled
// by c, which can be nil for default formatting.
func NewEncodeTransformer(c *Config) transform.Transformer {
	if c == nil {
		c = &defaultConfig
	}
	return &enctrans{
		c:     *c,
		state: needPrefix,
	}
}

type enctrans struct {
	c          Config
	state      encTransState
	wordCnt    uint
	groupCnt   uint
	wordidx    [3]int
	wordidxcnt int // remaining indexes in wordidx; wordidx[3-wordidxcnt:]
}

func (t *enctrans) Reset() {
	t.state = needPrefix
	t.wordCnt = 0
	t.groupCnt = 0
	t.wordidxcnt = 0
}

type encTransState uint8

const (
	needNothing = iota
	needPrefix
	needWordSep
	needGroupSep
	needSuffix
)

func (t *enctrans) strState() (str string, nextState encTransState) {
	switch t.state {
	case needPrefix:
		str = t.c.LinePrefix
	case needWordSep:
		str = t.c.WordSeparator
	case needGroupSep:
		str = t.c.GroupSeparator
	case needSuffix:
		str = t.c.LineSuffix
		nextState = needPrefix
	}
	return
}

func (t *enctrans) advState() {
	t.wordCnt++
	if t.wordCnt < t.c.WordsPerGroup {
		t.state = needWordSep
	} else {
		t.wordCnt = 0
		t.groupCnt++
		if t.groupCnt < t.c.GroupsPerLine {
			t.state = needGroupSep
		} else {
			t.groupCnt = 0
			t.state = needSuffix
		}
	}
}

// transformWords consumes words from wordidx copying the words with
// formatting into dst.
// On return, if err==nil, all words were consumed (wordidxcnt==0).
func (t *enctrans) transformWords(dst []byte) (nDst int, err error) {
	//log.Println("transformWords: len(dst)=",len(dst),"wordidxcnt=",t.wordidxcnt)
	for t.wordidxcnt > 0 {
		for t.state != needNothing {
			str, nextState := t.strState()
			if len(dst) < len(str) {
				return nDst, transform.ErrShortDst
			}
			n := copy(dst, str)
			dst = dst[n:]
			nDst += n
			t.state = nextState
		}
		word := wordList[t.wordidx[3-t.wordidxcnt]]
		n := len(word)
		if n < longestWord {
			if rlen := utf8.RuneLen(t.c.WordPadding); rlen > 0 {
				n += (longestWord - n) * rlen
			}
		}
		if len(dst) < n {
			return nDst, transform.ErrShortDst
		}
		n = copy(dst, word)
		t.wordidxcnt--
		dst = dst[n:]
		nDst += n
		if t.c.WordPadding != 0 {
			for i := n; i < longestWord; i++ {
				n = utf8.EncodeRune(dst, t.c.WordPadding)
				dst = dst[n:]
				nDst += n
			}
		}
		t.advState()
	}
	return nDst, nil
}

// Transform implements the transform.Transformer interface.
func (t *enctrans) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	//log.Printf("Transform(%d,%d,%t)\n", len(dst), len(src), atEOF)
	var n int
	for {
		if t.wordidxcnt > 0 {
			n, err = t.transformWords(dst)
			dst = dst[n:]
			nDst += n
			if err != nil {
				//log.Printf("\t\t\tRet1: (%d) %d, %d, %v\n", t.wordidxcnt, nDst, nSrc, err)
				return
			}
		}
		var x uint32
		switch {
		case len(src) >= 4:
			x = uint32(src[0])
			x |= uint32(src[1]) << 8
			x |= uint32(src[2]) << 16
			x |= uint32(src[3]) << 24
			src = src[4:]
			nSrc += 4

			t.wordidx[0] = int(x % base)
			t.wordidx[1] = int(x/base) % base
			t.wordidx[2] = int(x/base/base) % base
			t.wordidxcnt = 3
			//log.Printf("\t\tConsumed 4 bytes (%d, %d)", nDst, nSrc)
			//continue
		case len(src) == 0:
			//log.Printf("\t\t\tRet2: (%d) %d, %d, %v\n", t.wordidxcnt, nDst, nSrc, err)
			return
		case !atEOF:
			//log.Printf("\t\t!atEOF (%d, %d)", nDst, nSrc)
			err = transform.ErrShortSrc
			return
		default:
			x = 0
			n = len(src)
			for i := n - 1; i >= 0; i-- {
				x <<= 8
				x |= uint32(src[i])
			}
			t.wordidx[3-n] = int(x % base)
			if n >= 2 {
				t.wordidx[4-n] = int(x/base) % base
			}
			if n == 3 {
				t.wordidx[2] = base + int(x/base/base)%7
			}
			src = src[n:]
			nSrc += n
			t.wordidxcnt = n
			//log.Printf("\t\tatEOF (%d) (%d, %d)", t.wordidxcnt, nDst, nSrc)
			//continue
		}
	}
}

//

// NewDecoder returns a new io.Reader that will return the
// decoded bytes from mnemonic words in r. Unrecognized
// words in r will cause reads to return an error.
func NewDecoder(r io.Reader) io.Reader {
	t := NewDecodeTransformer()
	return transform.NewReader(r, t)
}

// NewDecodeWriter returns a new io.WriteCloser that will
// write decoded bytes from mnemonic words written to it.
// Unrecognized words will cause a write error. The user needs
// to call Close to flush unwritten bytes that may be buffered.
func NewDecodeWriter(w io.Writer) io.WriteCloser {
	t := NewDecodeTransformer()
	return transform.NewWriter(w, t)
}

// NewDecodeTransformer returns a new transform
// that decodes mnemonic words into the represented
// bytes. Unrecognized words will trigger an error.
func NewDecodeTransformer() transform.Transformer {
	return &dectrans{wordidx: make([]int, 0, 3)}
}

type dectrans struct {
	wordidx []int
	short   bool // last word in wordidx is/was short
}

func (t *dectrans) Reset() {
	t.wordidx = nil
	t.short = false
}

func (t *dectrans) transformWords(dst []byte) (int, error) {
	//log.Println("transformWords: len(dst)=",len(dst),"len(t.wordidx)=", len(t.wordidx))
	n := len(t.wordidx)
	if n == 3 && !t.short {
		n = 4
	}
	if len(dst) < n {
		return 0, transform.ErrShortDst
	}
	for len(t.wordidx) < 3 {
		t.wordidx = append(t.wordidx, 0)
	}
	x := uint32(t.wordidx[2])
	x *= base
	x += uint32(t.wordidx[1])
	x *= base
	x += uint32(t.wordidx[0])
	for i := 0; i < n; i++ {
		dst[i] = byte(x)
		x >>= 8
	}
	t.wordidx = t.wordidx[:0]
	return n, nil
}

type WordError interface {
	error
	Word() string
}

type UnexpectedWordError string
type UnexpectedEndWordError string
type UnknownWordError string

func (e UnexpectedWordError) Word() string    { return string(e) }
func (e UnexpectedEndWordError) Word() string { return string(e) }
func (e UnknownWordError) Word() string       { return string(e) }
func (e UnexpectedWordError) Error() string {
	return fmt.Sprintf("mnemonicode: unexpected word after short word: %q", string(e))
}
func (e UnexpectedEndWordError) Error() string {
	return fmt.Sprintf("mnemonicode: unexpected end word: %q", string(e))
}
func (e UnknownWordError) Error() string {
	return fmt.Sprintf("mnemonicode: unknown word: %q", string(e))
}

// Transform implements the transform.Transformer interface.
func (t *dectrans) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	//log.Printf("Transform(%d,%d,%t)\n", len(dst), len(src), atEOF)
	var n int
	for len(t.wordidx) > 0 || len(src) > 0 {
		for len(t.wordidx) < 3 {
			var word []byte
			var idx int
			//n, word, err = bufio.ScanWords(src, atEOF)
			n, word, err = scanWords(src, atEOF)
			src = src[n:]
			nSrc += n
			if err != nil {
				//log.Print("ScanWords error:", err)
				return
			}
			if word == nil {
				if atEOF {
					//log.Printf("atEOF (%d, %d) %d, %d", nDst, nSrc, n, len(src))
					n = len(src)
					src = src[n:]
					nSrc += n
					break
				}
				//log.Printf("\t\t!atEOF (%d, %d)", nDst, nSrc)
				err = transform.ErrShortSrc
				return
			}
			if t.short {
				err = UnexpectedWordError(word)
				//log.Print("short error:", err)
				return
			}
			idx, _, t.short, err = closestWordIdx(string(word), len(t.wordidx) == 2)
			if err != nil {
				//log.Print("closestWordIdx error:", err)
				return
			}
			t.wordidx = append(t.wordidx, idx)
		}
		if len(t.wordidx) > 0 {
			n, err = t.transformWords(dst)
			dst = dst[n:]
			nDst += n
			if n != 4 {
				//log.Println("transformWords returned:", n, err)
				//log.Println("len(t.wordidx):", len(t.wordidx), len(src))
			}
			if err != nil {
				//log.Printf("\t\t\tRet1: (%d) %d, %d, %v\n", len(t.wordidx), nDst, nSrc, err)
				return
			}
		}
	}
	return
}

//

const base = 1626

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
		result = append(result, wordList[i0], wordList[i1], wordList[i2])
	}
	if len(src) > 0 {
		x = 0
		for i := len(src) - 1; i >= 0; i-- {
			x <<= 8
			x |= uint32(src[i])
		}
		i := int(x % base)
		result = append(result, wordList[i])
		if len(src) >= 2 {
			i = int(x/base) % base
			result = append(result, wordList[i])
		}
		if len(src) == 3 {
			i = base + int(x/base/base)%7
			result = append(result, wordList[i])
		}
	}

	return result
}

func closestWordIdx(word string, shortok bool) (idx int, exact, short bool, err error) {
	word = strings.ToLower(word)
	if idx, exact = wordMap[word]; !exact {
		// TODO(dchapes): normalize unicode, remove accents, etc
		// TODO(dchapes): phonetic algorithm or other closest match
		err = UnknownWordError(word)
		return
	}
	if short = (idx >= base); short {
		idx -= base
		if !shortok {
			err = UnexpectedEndWordError(word)
		}
	}
	return
}

// DecodeWordList decodes the mnemonic words in src into bytes which are
// appended to dst.
func DecodeWordList(dst []byte, src []string) (result []byte, err error) {
	if n := (len(src)+2)/3*4 + len(dst); cap(dst) < n {
		result = make([]byte, len(dst), n)
		copy(result, dst)
	} else {
		result = dst
	}

	var idx [3]int
	for len(src) > 3 {
		if idx[0], _, _, err = closestWordIdx(src[0], false); err != nil {
			return nil, err
		}
		if idx[1], _, _, err = closestWordIdx(src[1], false); err != nil {
			return nil, err
		}
		if idx[2], _, _, err = closestWordIdx(src[2], false); err != nil {
			return nil, err
		}
		src = src[3:]
		x := uint32(idx[2])
		x *= base
		x += uint32(idx[1])
		x *= base
		x += uint32(idx[0])
		result = append(result, byte(x), byte(x>>8), byte(x>>16), byte(x>>24))
	}

	if len(src) > 0 {
		var short bool
		idx[1] = 0
		idx[2] = 0
		n := len(src)
		for i := 0; i < n; i++ {
			idx[i], _, short, err = closestWordIdx(src[i], i == 2)
			if err != nil {
				return nil, err
			}
		}
		x := uint32(idx[2])
		x *= base
		x += uint32(idx[1])
		x *= base
		x += uint32(idx[0])
		result = append(result, byte(x))
		if n > 1 {
			result = append(result, byte(x>>8))
		}
		if n > 2 {
			result = append(result, byte(x>>16))
			if !short {
				result = append(result, byte(x>>24))
			}
		}
	}

	/*
		for len(src) > 0 {
			short := false
			n := len(src)
			if n > 3 {
				n = 3
			}
			for i := 0; i < n; i++ {
				idx[i], _, err = closestWordIdx(src[i])
				if err != nil {
					return nil, err
				}
				if idx[i] >= base {
					if i != 2 || len(src) != 3 {
						return nil, UnexpectedEndWord(src[i])
					}
					short = true
					idx[i] -= base
				}
			}
			for i := n; i < 3; i++ {
				idx[i] = 0
			}
			src = src[n:]
			x := uint32(idx[2])
			x *= base
			x += uint32(idx[1])
			x *= base
			x += uint32(idx[0])
			result = append(result, byte(x))
			if n > 1 {
				result = append(result, byte(x>>8))
			}
			if n > 2 {
				result = append(result, byte(x>>16))
				if !short {
					result = append(result, byte(x>>24))
				}
			}
		}
	*/

	return result, nil
}
