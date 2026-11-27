package codephrase

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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

func TestVendoredEFFWordList(t *testing.T) {
	require.Len(t, effWords, 1296)
	assert.Equal(t, "acid", effWords[0])
	assert.Equal(t, "zoom", effWords[len(effWords)-1])
	digest := sha256.Sum256([]byte(strings.ReplaceAll(effWordList, "\r\n", "\n")))
	assert.Equal(t, "36ecca49e4fa20ca84b176c32f2e9c82f98f446585190e75f9879a95c08247bf", hex.EncodeToString(digest[:]))
	seen := make(map[string]struct{}, len(effWords))
	for _, word := range effWords {
		assert.Truef(t, isLowercaseListWord(word), "invalid word %q", word)
		_, duplicate := seen[word]
		assert.Falsef(t, duplicate, "duplicate word %q", word)
		seen[word] = struct{}{}
	}
}

func TestVendoredCodeWordList(t *testing.T) {
	require.Len(t, codeWords, 7223)
	assert.Equal(t, "about", codeWords[0])
	assert.Equal(t, "laos", codeWords[len(codeWords)-1])
	digest := sha256.Sum256([]byte(strings.ReplaceAll(codeWordList, "\r\n", "\n")))
	assert.Equal(t, "b407eaf72beeecb8d51a234492c132688fdd562128d9bf2405199e1c50aef2c3", hex.EncodeToString(digest[:]))
	seen := make(map[string]struct{}, len(codeWords))
	for _, word := range codeWords {
		assert.Regexp(t, `^[a-z]{3,8}$`, word)
		_, duplicate := seen[word]
		assert.Falsef(t, duplicate, "duplicate word %q", word)
		seen[word] = struct{}{}
	}
}

func TestLoadCodeWordsValidatesGenerationList(t *testing.T) {
	assert.Equal(t, codeWords, mustLoadCodeWords(strings.ReplaceAll(codeWordList, "\n", "\r\n")))
	for _, invalid := range []string{"ab", "yo-yo", "About", "about ", "abo\rut", codeWords[1]} {
		contents := strings.Replace(codeWordList, "about\n", invalid+"\n", 1)
		assert.Panics(t, func() { mustLoadCodeWords(contents) }, invalid)
	}
}

func TestLoadWordsAcceptsWindowsLineEndings(t *testing.T) {
	assert.Equal(t, []string{"acid", "acorn"}, mustLoadWords("test", "acid\r\nacorn\r\n", 2))
	assert.Panics(t, func() { mustLoadWords("test", "acid\racorn\r", 1) })
}

func TestGenerate(t *testing.T) {
	code, err := generate(zeroReader{})
	require.NoError(t, err)
	assert.Equal(t, "about-about-about", code)

	randomCode, err := Generate()
	require.NoError(t, err)
	words, ok := codeWordSequence(randomCode, 3)
	require.True(t, ok)
	require.Len(t, words, 3)
	for _, word := range words {
		assert.Contains(t, codeWords, word)
	}
	components, err := Parse(randomCode)
	require.NoError(t, err)
	assert.Equal(t, roomName(words[0]), components.RoomName)
	assert.Equal(t, strings.Join(words[1:], "-"), components.PAKEPassphrase)
}

func TestGenerateUsesShortListAndRejectsBiasedSamples(t *testing.T) {
	// 8191 is outside the 7223-word range; 7222 selects the last short word.
	input := append([]byte{0x1f, 0xff}, bytes.Repeat([]byte{0x1c, 0x36}, 3)...)
	code, err := generate(bytes.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, "laos-laos-laos", code)
}

func TestGenerateLetters(t *testing.T) {
	code, err := generateLetters(zeroReader{})
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaa-aaaaaaaa-aaaaaaaa", code)

	// Reject out-of-alphabet values rather than reducing them modulo 26.
	input := append([]byte{26, 31, 25}, make([]byte, 23)...)
	code, err = generateLetters(bytes.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, "zaaaaaaa-aaaaaaaa-aaaaaaaa", code)

	code, err = GenerateLetters()
	require.NoError(t, err)
	assert.Regexp(t, `^[a-z]{8}-[a-z]{8}-[a-z]{8}$`, code)
	components, err := Parse(code)
	require.NoError(t, err)
	assert.Equal(t, FormatThreeWord, components.Format)
	assert.Equal(t, code[9:], components.PAKEPassphrase)
}

func TestGenerateLettersPropagatesErrorsWithoutPartialCode(t *testing.T) {
	for _, reader := range []io.Reader{failingReader{}, bytes.NewReader(make([]byte, 9))} {
		code, err := generateLetters(reader)
		require.Error(t, err)
		assert.Empty(t, code)
		assert.ErrorContains(t, err, "generate croc code letter")
	}
}

func TestGenerateLettersForRelay(t *testing.T) {
	for index := 0; index < 4; index++ {
		code, err := GenerateLettersForRelay(index, 4)
		require.NoError(t, err)
		assert.Regexp(t, `^[a-z]{8}-[a-z]{8}-[a-z]{8}$`, code)
		got, err := RelayIndex(code, 4)
		require.NoError(t, err)
		assert.Equal(t, index, got)
	}
	_, err := GenerateLettersForRelay(0, 0)
	assert.ErrorIs(t, err, ErrInvalidRelayCount)
	for _, index := range []int{-1, 4} {
		_, err := GenerateLettersForRelay(index, 4)
		assert.ErrorIs(t, err, ErrInvalidRelayIndex)
	}
}

func TestGenerateAndParseSSH(t *testing.T) {
	code, err := generateWords(zeroReader{}, SSHCodeWordCount)
	require.NoError(t, err)
	assert.Equal(t, "about-about-about-about-about-about", code)

	components, err := ParseSSH(code)
	require.NoError(t, err)
	assert.Equal(t, sshRoomName("about-about"), components.RoomName)
	assert.Equal(t, "about-about-about-about", components.PAKEPassphrase)

	randomCode, err := GenerateSSH()
	require.NoError(t, err)
	words, ok := codeWordSequence(randomCode, SSHCodeWordCount)
	require.True(t, ok)
	assert.Len(t, words, SSHCodeWordCount)
	components, err = ParseSSH(randomCode)
	require.NoError(t, err)
	assert.Equal(t, sshRoomName(strings.Join(words[:2], "-")), components.RoomName)
	assert.Equal(t, strings.Join(words[2:], "-"), components.PAKEPassphrase)
}

func TestParseSSHAcceptsCurrentAndLegacyLists(t *testing.T) {
	for _, code := range []string{
		"about-search-other-the-and-laos",
		"acid-acorn-acre-acts-ahead-alien",
		"yo-yo-yo-yo-yo-yo-yo-yo-yo-yo-yo-yo",
	} {
		_, err := ParseSSH(code)
		require.NoError(t, err, code)
	}
}

func TestParseSSHSupportsHyphenatedWords(t *testing.T) {
	components, err := ParseSSH("yo-yo-acid-acorn-acre-acts-ahead")
	require.NoError(t, err)
	assert.Equal(t, sshRoomName("yo-yo-acid"), components.RoomName)
	assert.Equal(t, "acorn-acre-acts-ahead", components.PAKEPassphrase)
}

func TestParseSSHRejectsOtherCodeShapes(t *testing.T) {
	for _, code := range []string{
		"acid-acorn-acre",
		"acid-acorn-acre-acts-ahead",
		"acid-acorn-acre-acts-ahead-alien-extra",
		"acid-acorn-acre-acts-ahead-NOT",
		"about-search-other-the-and-zzzznotaword",
		"about-search-other-the-and-ab",
		"about-search-other-the-and-YOU",
	} {
		_, err := ParseSSH(code)
		assert.Error(t, err, code)
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
			name:           "letter code uses the first group as its room",
			secret:         "qjvmpxza-rktnbywe-hsfucold",
			format:         FormatThreeWord,
			roomSelector:   "qjvmpxza",
			pakePassphrase: "rktnbywe-hsfucold",
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

func sshRoomName(selector string) string {
	digest := sha256.Sum256([]byte(selector + "croc-ssh-v1"))
	return hex.EncodeToString(digest[:])
}
