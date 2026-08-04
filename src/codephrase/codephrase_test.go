package codephrase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
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

func TestVendoredWordLists(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		words     []string
		count     int
		firstWord string
		lastWord  string
		sha256    string
	}{
		{
			name:      "alpha",
			raw:       alphaWordList,
			words:     alphaWords,
			count:     1296,
			firstWord: "abbot",
			lastWord:  "zoom",
			sha256:    "f50e9890e62c5cfac535f51193914018591e49e50b56b38b8fd60bcbe7af8796",
		},
		{
			name:      "long",
			raw:       longWordList,
			words:     longWords,
			count:     17576,
			firstWord: "abandon",
			lastWord:  "zoom",
			sha256:    "21b00942246dc7f0ecf5321dc22bc4ce2326b51ea72ea55697d754601ca115d2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.words, tt.count)
			assert.Equal(t, tt.firstWord, tt.words[0])
			assert.Equal(t, tt.lastWord, tt.words[len(tt.words)-1])
			digest := sha256.Sum256([]byte(tt.raw))
			assert.Equal(t, tt.sha256, hex.EncodeToString(digest[:]))
			seen := make(map[string]struct{}, len(tt.words))
			for _, word := range tt.words {
				assert.Truef(t, isLowercaseWord(word), "invalid word %q", word)
				_, duplicate := seen[word]
				assert.Falsef(t, duplicate, "duplicate word %q", word)
				seen[word] = struct{}{}
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	code, err := generate(zeroReader{})
	require.NoError(t, err)
	assert.Equal(t, "abbot-abbot-abandon-abandon", code)

	randomCode, err := Generate()
	require.NoError(t, err)
	words := strings.Split(randomCode, "-")
	require.Len(t, words, 4)
	assert.Contains(t, alphaWords, words[0])
	assert.Contains(t, alphaWords, words[1])
	assert.Contains(t, longWords, words[2])
	assert.Contains(t, longWords, words[3])
}

func TestGeneratePropagatesRandomSourceErrors(t *testing.T) {
	_, err := generate(failingReader{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "generate croc code word 1")
	assert.ErrorContains(t, err, "random source failed")
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
			name:           "orchard street code",
			secret:         "abbot-abide-abandon-abandoned",
			format:         FormatFourWord,
			roomSelector:   "abbot-abide",
			pakePassphrase: "abandon-abandoned",
		},
		{
			name:           "arbitrary lowercase words reserve new format",
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
			secret:         "Word-word-word-word",
			format:         FormatLegacy,
			roomSelector:   "Word",
			pakePassphrase: "word-word-word",
		},
		{
			name:           "numeric character falls back",
			secret:         "word1-word-word-word",
			format:         FormatLegacy,
			roomSelector:   "word",
			pakePassphrase: "-word-word-word",
		},
		{
			name:           "empty component falls back",
			secret:         "word--word-word",
			format:         FormatLegacy,
			roomSelector:   "word",
			pakePassphrase: "-word-word",
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
	components, err := Parse("abbot-abide-abandon-abandoned")
	require.NoError(t, err)
	assert.Equal(
		t,
		"94707140c3581a1d897d27dd93462cdb7df85df84d7e9d7b874e8267fb1cee67",
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
