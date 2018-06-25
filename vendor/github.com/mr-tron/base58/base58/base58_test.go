package base58

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

type testValues struct {
	dec []byte
	enc string
}

var n = 5000000
var testPairs = make([]testValues, 0, n)

func initTestPairs() {
	if len(testPairs) > 0 {
		return
	}
	// pre-make the test pairs, so it doesn't take up benchmark time...
	data := make([]byte, 32)
	for i := 0; i < n; i++ {
		rand.Read(data)
		testPairs = append(testPairs, testValues{dec: data, enc: FastBase58Encoding(data)})
	}
}

func randAlphabet() *Alphabet {
	// Permutes [0, 127] and returns the first 58 elements.
	// Like (math/rand).Perm but using crypto/rand.
	var randomness [128]byte
	rand.Read(randomness[:])

	var bts [128]byte
	for i, r := range randomness {
		j := int(r) % (i + 1)
		bts[i] = bts[j]
		bts[j] = byte(i)
	}
	return NewAlphabet(string(bts[:58]))
}

func TestFastEqTrivialEncodingAndDecoding(t *testing.T) {
	for k := 0; k < 10; k++ {
		testEncDecLoop(t, randAlphabet())
	}
	testEncDecLoop(t, BTCAlphabet)
	testEncDecLoop(t, FlickrAlphabet)
}

func testEncDecLoop(t *testing.T, alph *Alphabet) {
	for j := 1; j < 256; j++ {
		var b = make([]byte, j)
		for i := 0; i < 100; i++ {
			rand.Read(b)
			fe := FastBase58EncodingAlphabet(b, alph)
			te := TrivialBase58EncodingAlphabet(b, alph)

			if fe != te {
				t.Errorf("encoding err: %#v", hex.EncodeToString(b))
			}

			fd, ferr := FastBase58DecodingAlphabet(fe, alph)
			if ferr != nil {
				t.Errorf("fast error: %v", ferr)
			}
			td, terr := TrivialBase58DecodingAlphabet(te, alph)
			if terr != nil {
				t.Errorf("trivial error: %v", terr)
			}

			if hex.EncodeToString(b) != hex.EncodeToString(td) {
				t.Errorf("decoding err: %s != %s", hex.EncodeToString(b), hex.EncodeToString(td))
			}
			if hex.EncodeToString(b) != hex.EncodeToString(fd) {
				t.Errorf("decoding err: %s != %s", hex.EncodeToString(b), hex.EncodeToString(fd))
			}
		}
	}
}

func BenchmarkTrivialBase58Encoding(b *testing.B) {
	initTestPairs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		TrivialBase58Encoding([]byte(testPairs[i].dec))
	}
}

func BenchmarkFastBase58Encoding(b *testing.B) {
	initTestPairs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		FastBase58Encoding(testPairs[i].dec)
	}
}

func BenchmarkTrivialBase58Decoding(b *testing.B) {
	initTestPairs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		TrivialBase58Decoding(testPairs[i].enc)
	}
}

func BenchmarkFastBase58Decoding(b *testing.B) {
	initTestPairs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		FastBase58Decoding(testPairs[i].enc)
	}
}
