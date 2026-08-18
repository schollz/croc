package codephrase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("random source failed")
}

func TestVendoredWordList(t *testing.T) {
	require.Len(t, effWords, 1296)
	assert.Equal(t, "acid", effWords[0])
	assert.Equal(t, "zoom", effWords[len(effWords)-1])
	digest := sha256.Sum256([]byte(effWordList))
	assert.Equal(t, "36ecca49e4fa20ca84b176c32f2e9c82f98f446585190e75f9879a95c08247bf", hex.EncodeToString(digest[:]))
	seen := make(map[string]struct{}, len(effWords))
	for _, word := range effWords {
		assert.Truef(t, isLowercaseListWord(word), "invalid word %q", word)
		_, duplicate := seen[word]
		assert.Falsef(t, duplicate, "duplicate word %q", word)
		seen[word] = struct{}{}
	}
}

func TestGenerate(t *testing.T) {
	code, err := generate(zeroReader{})
	require.NoError(t, err)
	assert.Equal(t, "acid-acid-acid", code)

	randomCode, err := Generate()
	require.NoError(t, err)
	words, ok := threeEFFWords(randomCode)
	require.True(t, ok)
	require.Len(t, words, 3)
	for _, word := range words {
		assert.Contains(t, effWords, word)
	}
}

func TestThreeEFFWordsSupportsHyphenatedWord(t *testing.T) {
	tests := []string{
		"yo-yo-acid-acorn",
		"acid-yo-yo-acorn",
		"acid-acorn-yo-yo",
		"yo-yo-yo-yo-yo-yo",
	}
	for _, secret := range tests {
		words, ok := threeEFFWords(secret)
		require.Truef(t, ok, "did not recognize %q", secret)
		assert.Contains(t, words, "yo-yo")

		components, err := Parse(secret)
		require.NoError(t, err)
		assert.Equal(t, FormatThreeWord, components.Format)
	}
}

func TestGeneratePropagatesRandomSourceErrors(t *testing.T) {
	_, err := generate(failingReader{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "generate croc code word 1")
	assert.ErrorContains(t, err, "random source failed")
}

func TestRelayIndexVectors(t *testing.T) {
	tests := map[string]int{
		"Word-word-word":           0,
		"acid-acorn-acre":          1,
		"poker-hedge-floss":        2,
		"custom-passphrase":        1,
		"1234-alpha-bravo-charlie": 2,
	}
	for code, want := range tests {
		got, err := RelayIndex(code, 3)
		require.NoError(t, err)
		assert.Equalf(t, want, got, "RelayIndex(%q, 3)", code)
	}
}

func TestRelayIndexRejectsInvalidCount(t *testing.T) {
	_, err := RelayIndex("valid-code", 0)
	assert.ErrorIs(t, err, ErrInvalidRelayCount)
}

func TestGenerateForRelayUsesNormalCodes(t *testing.T) {
	candidates := []string{"acid-acorn-acre", "poker-hedge-floss"}
	code, err := generateForRelay(2, 3, func() (string, error) {
		candidate := candidates[0]
		candidates = candidates[1:]
		return candidate, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "poker-hedge-floss", code)
}

func TestGenerateForRelayValidatesAndPropagatesErrors(t *testing.T) {
	_, err := generateForRelay(-1, 3, func() (string, error) { return "", nil })
	assert.ErrorIs(t, err, ErrInvalidRelayIndex)
	_, err = generateForRelay(3, 3, func() (string, error) { return "", nil })
	assert.ErrorIs(t, err, ErrInvalidRelayIndex)
	_, err = generateForRelay(0, 0, func() (string, error) { return "", nil })
	assert.ErrorIs(t, err, ErrInvalidRelayCount)
	want := errors.New("random source failed")
	_, err = generateForRelay(0, 3, func() (string, error) { return "", want })
	assert.ErrorIs(t, err, want)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name           string
		secret         string
		format         Format
		roomSelector   string
		pakePassphrase string
	}{
		{
			name:           "EFF three-word code uses the complete first word as the room",
			secret:         "poker-hedge-floss",
			format:         FormatThreeWord,
			roomSelector:   "poker",
			pakePassphrase: "hedge-floss",
		},
		{
			name:           "arbitrary lowercase words reserve three-word format",
			secret:         "foo-bar-baz",
			format:         FormatThreeWord,
			roomSelector:   "foo",
			pakePassphrase: "bar-baz",
		},
		{
			name:           "previous four-word code remains compatible",
			secret:         "abbot-abide-abandon-abandoned",
			format:         FormatFourWord,
			roomSelector:   "abbot-abide",
			pakePassphrase: "abandon-abandoned",
		},
		{
			name:           "arbitrary lowercase words reserve four-word format",
			secret:         "foo-bar-baz-qux",
			format:         FormatFourWord,
			roomSelector:   "foo-bar",
			pakePassphrase: "baz-qux",
		},
		{
			name:           "legacy generated code",
			secret:         "1234-alpha-bravo-charlie",
			format:         FormatLegacy,
			roomSelector:   "1234",
			pakePassphrase: "alpha-bravo-charlie",
		},
		{
			name:           "legacy compact code",
			secret:         "1234-alpha-bravo",
			format:         FormatLegacy,
			roomSelector:   "1234",
			pakePassphrase: "alpha-bravo",
		},
		{
			name:           "legacy custom code",
			secret:         "custom-passphrase",
			format:         FormatLegacy,
			roomSelector:   "cust",
			pakePassphrase: "m-passphrase",
		},
		{
			name:           "uppercase word falls back",
			secret:         "Word-word-word",
			format:         FormatLegacy,
			roomSelector:   "Word",
			pakePassphrase: "word-word",
		},
		{
			name:           "numeric character falls back",
			secret:         "word1-word-word",
			format:         FormatLegacy,
			roomSelector:   "word",
			pakePassphrase: "-word-word",
		},
		{
			name:           "empty component falls back",
			secret:         "word--word",
			format:         FormatLegacy,
			roomSelector:   "word",
			pakePassphrase: "-word",
		},
		{
			name:           "extra component falls back",
			secret:         "word-word-word-word-extra",
			format:         FormatLegacy,
			roomSelector:   "word",
			pakePassphrase: "word-word-word-extra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components, err := Parse(tt.secret)
			require.NoError(t, err)
			assert.Equal(t, tt.format, components.Format)
			assert.Equal(t, roomName(tt.roomSelector), components.RoomName)
			assert.Equal(t, tt.pakePassphrase, components.PAKEPassphrase)
		})
	}
}

func TestParseUsesStableRoomHash(t *testing.T) {
	components, err := Parse("poker-hedge-floss")
	require.NoError(t, err)
	assert.Equal(
		t,
		"fced940f7e6edf059a837d10515c2b095fc4e9d9a079ed62229b0fb8ddba8be1",
		components.RoomName,
	)
}

func TestParseRejectsShortCode(t *testing.T) {
	_, err := Parse("abcde")
	assert.ErrorIs(t, err, ErrCodeTooShort)
	assert.EqualError(t, err, "code is too short (must be at least 6 characters)")
}

func roomName(selector string) string {
	digest := sha256.Sum256([]byte(selector + "croc"))
	return hex.EncodeToString(digest[:])
}
