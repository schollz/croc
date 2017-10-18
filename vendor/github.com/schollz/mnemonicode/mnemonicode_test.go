package mnemonicode

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/transform"
)

func TestWordsReq(t *testing.T) {
	for i, n := range []int{0, 1, 2, 3, 3, 4, 5, 6, 6, 7, 8, 9, 9, 10} {
		r := WordsRequired(i)
		if r != n {
			t.Errorf("WordsRequired(%d) returned %d, expected %d", i, r, n)
		}
	}
}

var testData = []struct {
	hex   string
	words []string
}{
	{"01", []string{"acrobat"}},
	{"0102", []string{"opera", "academy"}},
	{"010203", []string{"kayak", "cement", "ego"}},
	{"01020304", []string{"papa", "twist", "alpine"}},
	{"0102030405", []string{"papa", "twist", "alpine", "admiral"}},
	{"010203040506", []string{"papa", "twist", "alpine", "shine", "academy"}},
	{"01020304050607", []string{"papa", "twist", "alpine", "chess", "flute", "ego"}},
	{"0102030405060708", []string{"papa", "twist", "alpine", "content", "sailor", "athena"}},
	{"00", []string{"academy"}},
	{"5A06", []string{"academy", "acrobat"}},
	{"FE5D28", []string{"academy", "acrobat", "fax"}},
	{"A2B55000", []string{"academy", "acrobat", "active"}},
	{"A2B5500003", []string{"academy", "acrobat", "active", "actor"}},
	{"A2B550006B19", []string{"academy", "acrobat", "active", "actor", "adam"}},
	{"A2B550000F7128", []string{"academy", "acrobat", "active", "actor", "adam", "fax"}},
	{"A2B550009FCFC900", []string{"academy", "acrobat", "active", "actor", "adam", "admiral"}},
	{"FF", []string{"exact"}},
	{"FFFF", []string{"nevada", "archive"}},
	{"FFFFFF", []string{"claudia", "photo", "yes"}},
	{"FFFFFFFF", []string{"natural", "analyze", "verbal"}},
	{"123456789ABCDEF123456789ABCDEF012345", []string{
		"plastic", "roger", "vincent", "pilgrim", "flame", "secure", "apropos", "polka", "earth", "radio", "modern", "aladdin", "marion", "airline"}},
}

func compareWordList(tb testing.TB, expected, got []string, args ...interface{}) {
	fail := false
	if len(expected) != len(got) {
		fail = true
	}
	for i := 0; !fail && i < len(expected); i++ {
		fail = expected[i] != got[i]
	}
	if fail {
		prefix := ""
		if len(args) > 0 {
			prefix += fmt.Sprintln(args...)
			prefix = prefix[:len(prefix)-1] + ": "
		}
		tb.Errorf("%vexpected %v, got %v", prefix, expected, got)
	}
}

func TestEncodeWordList(t *testing.T) {
	var result []string
	for i, d := range testData {
		raw, err := hex.DecodeString(d.hex)
		if err != nil {
			t.Fatal("bad test data:", i, err)
		}
		result = EncodeWordList(result, raw)
		compareWordList(t, d.words, result, i, d.hex)
		result = result[:0]
	}
}

func TestDecodeWordList(t *testing.T) {
	var result []byte
	var err error
	for i, d := range testData {
		raw, _ := hex.DecodeString(d.hex)
		result, err = DecodeWordList(result, d.words)
		if err != nil {
			t.Errorf("%2d %v failed: %v", i, d.words, err)
			continue
		}
		if !bytes.Equal(raw, result) {
			t.Errorf("%2d %v expected %v got %v", i, d.words, raw, result)
		}
		result = result[:0]
	}
}

func TestEncodeTransformer(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.GroupSeparator = " "
	enc := NewEncodeTransformer(cfg)
	for i, d := range testData {
		raw, err := hex.DecodeString(d.hex)
		if err != nil {
			t.Fatal("bad test data:", i, err)
		}
		result, _, err := transform.Bytes(enc, raw)
		if err != nil {
			t.Errorf("%2d %v failed: %v", i, d.words, err)
			continue
		}
		//t.Logf("%q", result)
		words := strings.Fields(string(result))
		compareWordList(t, d.words, words, i, d.hex)
	}

}

func TestDecodeTransformer(t *testing.T) {
	dec := NewDecodeTransformer()
	for i, d := range testData {
		raw, _ := hex.DecodeString(d.hex)
		words := strings.Join(d.words, " ")
		result, _, err := transform.Bytes(dec, []byte(words))
		if err != nil {
			t.Errorf("%2d %v failed: %v", i, d.words, err)
			continue
		}
		if !bytes.Equal(raw, result) {
			t.Errorf("%2d %v expected %v got %v", i, d.words, raw, result)
		}
	}
}

func TestEncodeFormatting(t *testing.T) {
	raw, _ := hex.DecodeString(testData[20].hex)
	input := string(raw)
	//words := testData[20].words
	tests := []struct {
		cfg       *Config
		formatted string
	}{
		{nil, "plastic roger   vincent - pilgrim flame   secure  - apropos polka   earth  \nradio   modern  aladdin - marion  airline"},
		{&Config{
			LinePrefix:     "{P}",
			LineSuffix:     "{S}\n",
			WordSeparator:  "{w}",
			GroupSeparator: "{g}",
			WordsPerGroup:  2,
			GroupsPerLine:  2,
			WordPadding:    '·',
		},
			`{P}plastic{w}roger··{g}vincent{w}pilgrim{S}
{P}flame··{w}secure·{g}apropos{w}polka··{S}
{P}earth··{w}radio··{g}modern·{w}aladdin{S}
{P}marion·{w}airline`},
	}
	for i, d := range tests {
		enc := NewEncodeTransformer(d.cfg)
		result, _, err := transform.String(enc, input)
		if err != nil {
			t.Errorf("%2d transform failed: %v", i, err)
			continue
		}
		if result != d.formatted {
			t.Errorf("%2d expected:\n%q\ngot:\n%q", i, d.formatted, result)
		}
	}
}

func BenchmarkEncodeWordList(b *testing.B) {
	// the list of all known words (except the short end words)
	data, err := DecodeWordList(nil, wordList[:base])
	if err != nil {
		b.Fatal("DecodeWordList failed:", err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	var words []string
	for i := 0; i < b.N; i++ {
		words = EncodeWordList(words[:0], data)
	}
}

func BenchmarkDencodeWordList(b *testing.B) {
	b.ReportAllocs()
	var buf []byte
	var err error
	// decode the list of all known words (except the short end words)
	for i := 0; i < b.N; i++ {
		buf, err = DecodeWordList(buf[:0], wordList[:base])
		if err != nil {
			b.Fatal("DecodeWordList failed:", err)
		}
	}
	b.SetBytes(int64(len(buf)))
}

func BenchmarkEncodeTransformer(b *testing.B) {
	// the list of all known words (except the short end words)
	data, err := DecodeWordList(nil, wordList[:base])
	if err != nil {
		b.Fatal("DecodeWordList failed:", err)
	}
	enc := NewEncodeTransformer(nil)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := transform.Bytes(enc, data)
		if err != nil {
			b.Fatal("encode transformer error:", err)
		}
	}
}

func BenchmarkDecodeTransformer(b *testing.B) {
	data, err := DecodeWordList(nil, wordList[:base])
	if err != nil {
		b.Fatal("DecodeWordList failed:", err)
	}
	enc := NewEncodeTransformer(nil)
	words, _, err := transform.Bytes(enc, data)
	if err != nil {
		b.Fatal("encode transformer error:", err)
	}
	b.SetBytes(int64(len(data)))
	dec := NewDecodeTransformer()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := transform.Bytes(dec, words)
		if err != nil {
			b.Fatal("decode transformer error:", err)
		}
	}
}
