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
	codeWordCount        = 7223
	effWordCount         = 1296
	roomHashSuffix       = "croc"
	sshRoomHashSuffix    = "croc-ssh-v1"
	SSHCodeWordCount     = 6
	sshCodeRoomWordCount = 2
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

// SSHComponents contains the rendezvous and PAKE inputs for a croc SSH
// invitation. SSH codes deliberately use more entropy than file-transfer
// codes: two words select an unguessable relay room and four words remain in
// the PAKE password.
type SSHComponents struct {
	RoomName       string
	PAKEPassphrase string
}

//go:embed wordlists/eff-short-wordlist-1.txt
var effWordList string

//go:embed wordlists/google-english-no-swears.txt
var codeWordList string

var (
	codeWords   = mustLoadCodeWords(codeWordList)
	codeWordSet = makeWordSet(codeWords)

	// Retain the EFF list to parse older invitations, including "yo-yo".
	effWords   = mustLoadWords("EFF Short Wordlist #1", effWordList, effWordCount)
	effWordSet = makeWordSet(effWords)
)

func mustLoadCodeWords(contents string) []string {
	words := mustLoadWords("Google English", contents, codeWordCount)
	for _, word := range words {
		if len(word) < 3 || !isLowercaseWord(word) {
			panic(fmt.Sprintf("Google English word list contains invalid word %q", word))
		}
	}
	return words
}

func mustLoadWords(name, contents string, expected int) []string {
	// Git may check out the embedded text with CRLF on Windows. Normalize only
	// complete CRLF sequences so other control characters remain invalid.
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
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

// Generate returns a cryptographically random three-word code.
func Generate() (string, error) {
	return generate(rand.Reader)
}

// GenerateLetters returns three groups of eight cryptographically random
// lowercase ASCII letters, separated by hyphens.
func GenerateLetters() (string, error) {
	return generateLetters(rand.Reader)
}

// GenerateLettersForRelay returns a letter code assigned to relayIndex.
func GenerateLettersForRelay(relayIndex, relayCount int) (string, error) {
	return generateForRelay(relayIndex, relayCount, GenerateLetters)
}

// GenerateSSH returns a cryptographically random six-word SSH invitation.
func GenerateSSH() (string, error) {
	return generateWords(rand.Reader, SSHCodeWordCount)
}

// GenerateForRelay returns a normal three-word code assigned to relayIndex
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
	return generateWords(reader, 3)
}

func generateLetters(reader io.Reader) (string, error) {
	var code strings.Builder
	for i := 0; i < 24; i++ {
		if i > 0 && i%8 == 0 {
			code.WriteByte('-')
		}
		index, err := rand.Int(reader, big.NewInt(26))
		if err != nil {
			return "", fmt.Errorf("generate croc code letter %d: %w", i+1, err)
		}
		code.WriteByte('a' + byte(index.Int64()))
	}
	return code.String(), nil
}

func generateWords(reader io.Reader, count int) (string, error) {
	if count <= 0 {
		return "", errors.New("code word count must be positive")
	}
	words := make([]string, count)
	for i := range words {
		index, err := rand.Int(reader, big.NewInt(int64(len(codeWords))))
		if err != nil {
			return "", fmt.Errorf("generate croc code word %d: %w", i+1, err)
		}
		words[i] = codeWords[index.Int64()]
	}
	return strings.Join(words, "-"), nil
}

// ParseSSH resolves a six-word SSH invitation into its relay room and PAKE
// password. New codes use the Google English list; older EFF invitations
// remain accepted, including the hyphenated word "yo-yo".
func ParseSSH(secret string) (SSHComponents, error) {
	words, ok := codeWordSequence(secret, SSHCodeWordCount)
	if !ok {
		words, ok = effWordSequence(secret, SSHCodeWordCount)
	}
	if !ok {
		return SSHComponents{}, fmt.Errorf("invalid SSH code: expected %d words from croc's word lists", SSHCodeWordCount)
	}
	roomSelector := strings.Join(words[:sshCodeRoomWordCount], "-")
	digest := sha256.Sum256([]byte(roomSelector + sshRoomHashSuffix))
	return SSHComponents{
		RoomName:       hex.EncodeToString(digest[:]),
		PAKEPassphrase: strings.Join(words[sshCodeRoomWordCount:], "-"),
	}, nil
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

// threeEFFWords recognizes older codes even though the EFF list contains
// the word "yo-yo", which uses the same hyphen as croc's word separator.
func threeEFFWords(secret string) ([]string, bool) {
	return effWordSequence(secret, 3)
}

func codeWordSequence(secret string, count int) ([]string, bool) {
	words, ok := lowercaseWords(secret, count)
	if !ok {
		return nil, false
	}
	for _, word := range words {
		if _, ok := codeWordSet[word]; !ok {
			return nil, false
		}
	}
	return words, true
}

func effWordSequence(secret string, count int) ([]string, bool) {
	if count <= 0 {
		return nil, false
	}
	parts := strings.Split(secret, "-")
	if len(parts) < count || len(parts) > count*2 {
		return nil, false
	}

	words := make([]string, 0, count)
	var parse func(int) bool
	parse = func(start int) bool {
		if len(words) == count {
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
	parts := strings.SplitSeq(word, "-")
	for part := range parts {
		if !isLowercaseWord(part) {
			return false
		}
	}
	return true
}
