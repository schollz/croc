// Package codephrase generates croc codes and resolves them into relay-room
// and PAKE components.
package codephrase

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const (
	effWordCount   = 1296
	roomHashSuffix = "croc"
)

// Format identifies how a croc code is divided into its room and PAKE parts.
type Format string

const (
	// FormatLegacy uses the first four bytes for the room and bytes after the
	// fifth byte for PAKE.
	FormatLegacy Format = "legacy"
	// FormatThreeWord uses the first word for the room and the final two words
	// for PAKE.
	FormatThreeWord Format = "three-word"
	// FormatFourWord uses the first two words for the room and the final two
	// words for PAKE. It is retained for compatibility with older codes.
	FormatFourWord Format = "four-word"
)

// ErrCodeTooShort is returned when a croc code is shorter than six bytes.
var ErrCodeTooShort = errors.New("code is too short (must be at least 6 characters)")

// ErrInvalidRelayCount is returned when a code cannot be assigned because the
// public relay pool is empty.
var ErrInvalidRelayCount = errors.New("relay count must be positive")

// ErrInvalidRelayIndex is returned when a requested relay is outside the pool.
var ErrInvalidRelayIndex = errors.New("relay index is outside the relay pool")

// Components contains the protocol inputs derived from a user-facing croc
// code. RoomName is ready to send to a relay.
type Components struct {
	RoomName       string
	PAKEPassphrase string
	Format         Format
}

//go:embed wordlists/eff-short-wordlist-1.txt
var effWordList string

var (
	effWords   = mustLoadWords("EFF Short Wordlist #1", effWordList, effWordCount)
	effWordSet = makeWordSet(effWords)
)

func mustLoadWords(name, contents string, expected int) []string {
	words := strings.Split(strings.TrimSuffix(contents, "\n"), "\n")
	if len(words) != expected {
		panic(fmt.Sprintf("%s word list has %d entries; want %d", name, len(words), expected))
	}
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		if !isLowercaseListWord(word) {
			panic(fmt.Sprintf("%s word list contains invalid word %q", name, word))
		}
		if _, exists := seen[word]; exists {
			panic(fmt.Sprintf("%s word list contains duplicate word %q", name, word))
		}
		seen[word] = struct{}{}
	}
	return words
}

func makeWordSet(words []string) map[string]struct{} {
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		set[word] = struct{}{}
	}
	return set
}

// Generate returns a cryptographically random three-word EFF code.
func Generate() (string, error) {
	return generate(rand.Reader)
}

// GenerateForRelay returns a normal three-word EFF code assigned to relayIndex
// by RelayIndex. On average it generates relayCount candidates.
func GenerateForRelay(relayIndex, relayCount int) (string, error) {
	return generateForRelay(relayIndex, relayCount, Generate)
}

func generateForRelay(relayIndex, relayCount int, generator func() (string, error)) (string, error) {
	if relayCount <= 0 {
		return "", ErrInvalidRelayCount
	}
	if relayIndex < 0 || relayIndex >= relayCount {
		return "", ErrInvalidRelayIndex
	}
	for {
		code, err := generator()
		if err != nil {
			return "", err
		}
		index, err := RelayIndex(code, relayCount)
		if err != nil {
			return "", err
		}
		if index == relayIndex {
			return code, nil
		}
	}
}

// RelayIndex deterministically assigns the exact UTF-8 bytes of code to an
// entry in an ordered relay pool. The complete SHA-256 digest is interpreted
// as an unsigned big-endian integer before taking the modulo.
func RelayIndex(code string, relayCount int) (int, error) {
	if relayCount <= 0 {
		return 0, ErrInvalidRelayCount
	}
	digest := sha256.Sum256([]byte(code))
	value := new(big.Int).SetBytes(digest[:])
	value.Mod(value, big.NewInt(int64(relayCount)))
	return int(value.Int64()), nil
}

func generate(reader io.Reader) (string, error) {
	words := make([]string, 3)
	for i := range words {
		index, err := rand.Int(reader, big.NewInt(int64(len(effWords))))
		if err != nil {
			return "", fmt.Errorf("generate croc code word %d: %w", i+1, err)
		}
		words[i] = effWords[index.Int64()]
	}
	return strings.Join(words, "-"), nil
}

// Parse resolves a croc code into the relay room and PAKE passphrase used by
// the protocol. Exactly three lowercase ASCII words use the current three-word
// format. Four lowercase ASCII words keep compatibility with the previous
// four-word format; every other valid code uses croc's legacy byte-based split.
func Parse(secret string) (Components, error) {
	if len(secret) < 6 {
		return Components{}, ErrCodeTooShort
	}

	roomSelector := secret[:4]
	passphrase := secret[5:]
	format := FormatLegacy
	if words, ok := threeEFFWords(secret); ok {
		roomSelector = words[0]
		passphrase = strings.Join(words[1:], "-")
		format = FormatThreeWord
	} else if words, ok := lowercaseWords(secret, 3); ok {
		roomSelector = words[0]
		passphrase = strings.Join(words[1:], "-")
		format = FormatThreeWord
	} else if words, ok := lowercaseWords(secret, 4); ok {
		roomSelector = strings.Join(words[:2], "-")
		passphrase = strings.Join(words[2:], "-")
		format = FormatFourWord
	}

	digest := sha256.Sum256([]byte(roomSelector + roomHashSuffix))
	return Components{
		RoomName:       hex.EncodeToString(digest[:]),
		PAKEPassphrase: passphrase,
		Format:         format,
	}, nil
}

// threeEFFWords recognizes generated codes even though the EFF list contains
// the word "yo-yo", which uses the same hyphen as croc's word separator.
func threeEFFWords(secret string) ([]string, bool) {
	parts := strings.Split(secret, "-")
	if len(parts) < 3 || len(parts) > 6 {
		return nil, false
	}

	words := make([]string, 0, 3)
	var parse func(int) bool
	parse = func(start int) bool {
		if len(words) == 3 {
			return start == len(parts)
		}
		for end := start + 1; end <= len(parts); end++ {
			word := strings.Join(parts[start:end], "-")
			if _, ok := effWordSet[word]; !ok {
				continue
			}
			words = append(words, word)
			if parse(end) {
				return true
			}
			words = words[:len(words)-1]
		}
		return false
	}
	if !parse(0) {
		return nil, false
	}
	return words, true
}

func lowercaseWords(secret string, count int) ([]string, bool) {
	words := strings.Split(secret, "-")
	if len(words) != count {
		return nil, false
	}
	for _, word := range words {
		if !isLowercaseWord(word) {
			return nil, false
		}
	}
	return words, true
}

func isLowercaseWord(word string) bool {
	if word == "" {
		return false
	}
	for _, char := range word {
		if char < 'a' || char > 'z' {
			return false
		}
	}
	return true
}

func isLowercaseListWord(word string) bool {
	parts := strings.Split(word, "-")
	for _, part := range parts {
		if !isLowercaseWord(part) {
			return false
		}
	}
	return true
}
