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
	alphaWordCount = 1296
	longWordCount  = 17576
	roomHashSuffix = "croc"
)

// Format identifies how a croc code is divided into its room and PAKE parts.
type Format string

const (
	// FormatLegacy uses the first four bytes for the room and bytes after the
	// fifth byte for PAKE.
	FormatLegacy Format = "legacy"
	// FormatFourWord uses the first two words for the room and the final two
	// words for PAKE.
	FormatFourWord Format = "four-word"
)

// ErrCodeTooShort is returned when a croc code is shorter than six bytes.
var ErrCodeTooShort = errors.New("code is too short (must be at least 6 characters)")

// Components contains the protocol inputs derived from a user-facing croc
// code. RoomName is ready to send to a relay.
type Components struct {
	RoomName       string
	PAKEPassphrase string
	Format         Format
}

//go:embed wordlists/orchard-street-alpha.txt
var alphaWordList string

//go:embed wordlists/orchard-street-long.txt
var longWordList string

var (
	alphaWords = mustLoadWords("Orchard Street Alpha", alphaWordList, alphaWordCount)
	longWords  = mustLoadWords("Orchard Street Long", longWordList, longWordCount)
)

func mustLoadWords(name, contents string, expected int) []string {
	words := strings.Split(strings.TrimSuffix(contents, "\n"), "\n")
	if len(words) != expected {
		panic(fmt.Sprintf("%s word list has %d entries; want %d", name, len(words), expected))
	}
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		if !isLowercaseWord(word) {
			panic(fmt.Sprintf("%s word list contains invalid word %q", name, word))
		}
		if _, exists := seen[word]; exists {
			panic(fmt.Sprintf("%s word list contains duplicate word %q", name, word))
		}
		seen[word] = struct{}{}
	}
	return words
}

// Generate returns a cryptographically random Alpha-Alpha-Long-Long code.
func Generate() (string, error) {
	return generate(rand.Reader)
}

func generate(reader io.Reader) (string, error) {
	lists := [][]string{alphaWords, alphaWords, longWords, longWords}
	words := make([]string, len(lists))
	for i, list := range lists {
		index, err := rand.Int(reader, big.NewInt(int64(len(list))))
		if err != nil {
			return "", fmt.Errorf("generate croc code word %d: %w", i+1, err)
		}
		words[i] = list[index.Int64()]
	}
	return strings.Join(words, "-"), nil
}

// Parse resolves a croc code into the relay room and PAKE passphrase used by
// the protocol. Exactly four lowercase ASCII words use the four-word format;
// every other valid code keeps croc's legacy byte-based split.
func Parse(secret string) (Components, error) {
	if len(secret) < 6 {
		return Components{}, ErrCodeTooShort
	}

	roomSelector := secret[:4]
	passphrase := secret[5:]
	format := FormatLegacy
	if words, ok := fourLowercaseWords(secret); ok {
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

func fourLowercaseWords(secret string) ([]string, bool) {
	words := strings.Split(secret, "-")
	if len(words) != 4 {
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
