// Package storecrypto defines the versioned, client-side encrypted format used
// by croc's asynchronous stored transfers.
package storecrypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	Version       = 1
	Protocol      = "croc-store-v1"
	ChunkSize     = 4 * 1024 * 1024
	KeySize       = 32
	TransferIDLen = 16
	MaxFiles      = 100
)

var rawURL = base64.RawURLEncoding

// Manifest is encrypted before it is uploaded. The storage service never sees
// file names, modification times, sizes, or hashes.
type Manifest struct {
	Version   int            `json:"v"`
	ChunkSize int            `json:"cs"`
	Files     []ManifestFile `json:"f"`
}

// ManifestFile describes one regular file in a stored transfer.
type ManifestFile struct {
	Name       string    `json:"n"`
	Size       int64     `json:"s"`
	Modified   time.Time `json:"m"`
	SHA256     string    `json:"h"`
	FirstChunk int       `json:"fc"`
	ChunkCount int       `json:"cc"`
}

// ChunkRef identifies a chunk and supplies the authenticated context required
// to decrypt it.
type ChunkRef struct {
	ObjectIndex int
	FileIndex   int
	FileChunk   int
	PlainSize   int
}

// Share contains the public storage origin, opaque object ID, and secret
// master key encoded by a browser URL or CLI token.
type Share struct {
	Origin    string
	ID        string
	MasterKey []byte
}

// GenerateKey returns a fresh 256-bit transfer key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate stored-transfer key: %w", err)
	}
	return key, nil
}

// GenerateTransferID returns a URL-safe identifier containing 128 bits of
// entropy. Identifiers are public and are not decryption keys.
func GenerateTransferID() (string, error) {
	id := make([]byte, TransferIDLen)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generate transfer id: %w", err)
	}
	return rawURL.EncodeToString(id), nil
}

func derive(master []byte, label string) ([]byte, error) {
	if len(master) != KeySize {
		return nil, fmt.Errorf("stored-transfer key must be %d bytes", KeySize)
	}
	key, err := hkdf.Key(sha256.New, master, nil, Protocol+"/"+label, KeySize)
	if err != nil {
		return nil, fmt.Errorf("derive %s key: %w", label, err)
	}
	return key, nil
}

// RedeemCapability derives a capability that authorizes reading ciphertext but
// cannot decrypt it.
func RedeemCapability(master []byte) ([]byte, error) {
	return derive(master, "redeem")
}

// CapabilityVerifier returns the value stored by the service for a capability.
func CapabilityVerifier(capability []byte) []byte {
	sum := sha256.Sum256(capability)
	return sum[:]
}

func aeadFor(master []byte, label string) (cipher.AEAD, error) {
	key, err := derive(master, label)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create stored-transfer cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func seal(master []byte, label string, plaintext, aad []byte, random io.Reader) ([]byte, error) {
	aead, err := aeadFor(master, label)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(random, nonce); err != nil {
		return nil, fmt.Errorf("generate stored-transfer nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

func open(master []byte, label string, ciphertext, aad []byte) ([]byte, error) {
	aead, err := aeadFor(master, label)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("stored-transfer ciphertext is too short")
	}
	plaintext, err := aead.Open(
		nil,
		ciphertext[:aead.NonceSize()],
		ciphertext[aead.NonceSize():],
		aad,
	)
	if err != nil {
		return nil, errors.New("stored-transfer authentication failed")
	}
	return plaintext, nil
}

func manifestAAD(id string) []byte {
	return []byte(Protocol + "\x00" + id + "\x00manifest")
}

func chunkAAD(id string, ref ChunkRef) []byte {
	prefix := []byte(Protocol + "\x00" + id + "\x00chunk\x00")
	data := make([]byte, len(prefix)+16)
	copy(data, prefix)
	offset := len(prefix)
	binary.BigEndian.PutUint32(data[offset:], uint32(ref.ObjectIndex))
	binary.BigEndian.PutUint32(data[offset+4:], uint32(ref.FileIndex))
	binary.BigEndian.PutUint32(data[offset+8:], uint32(ref.FileChunk))
	binary.BigEndian.PutUint32(data[offset+12:], uint32(ref.PlainSize))
	return data
}

// SealManifest encrypts a validated manifest.
func SealManifest(master []byte, id string, manifest Manifest) ([]byte, error) {
	if err := ValidateManifest(manifest, 1<<40); err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode stored-transfer manifest: %w", err)
	}
	return SealManifestJSON(master, id, plaintext)
}

// OpenManifest authenticates, decrypts, and validates a manifest.
func OpenManifest(master []byte, id string, ciphertext []byte, maxBytes int64) (Manifest, error) {
	plaintext, err := OpenManifestJSON(master, id, ciphertext, maxBytes)
	if err != nil {
		return Manifest{}, err
	}
	return decodeManifest(plaintext, maxBytes)
}

// SealManifestJSON validates and encrypts a JSON-encoded manifest.
func SealManifestJSON(master []byte, id string, plaintext []byte) ([]byte, error) {
	if _, err := decodeManifest(plaintext, 1<<40); err != nil {
		return nil, err
	}
	return seal(master, "manifest", plaintext, manifestAAD(id), rand.Reader)
}

// OpenManifestJSON authenticates and decrypts a manifest, returning its
// validated JSON representation.
func OpenManifestJSON(master []byte, id string, ciphertext []byte, maxBytes int64) ([]byte, error) {
	plaintext, err := open(master, "manifest", ciphertext, manifestAAD(id))
	if err != nil {
		return nil, err
	}
	if _, err = decodeManifest(plaintext, maxBytes); err != nil {
		return nil, err
	}
	return plaintext, nil
}

func decodeManifest(plaintext []byte, maxBytes int64) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode stored-transfer manifest: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return Manifest{}, errors.New("stored-transfer manifest has trailing data")
	}
	if err := ValidateManifest(manifest, maxBytes); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// SealChunk encrypts one file chunk with position-binding associated data.
func SealChunk(master []byte, id string, ref ChunkRef, plaintext []byte) ([]byte, error) {
	if err := validateChunkRef(ref, len(plaintext)); err != nil {
		return nil, err
	}
	return seal(master, "data", plaintext, chunkAAD(id, ref), rand.Reader)
}

// OpenChunk authenticates and decrypts one file chunk.
func OpenChunk(master []byte, id string, ref ChunkRef, ciphertext []byte) ([]byte, error) {
	if err := validateChunkRef(ref, ref.PlainSize); err != nil {
		return nil, err
	}
	plaintext, err := open(master, "data", ciphertext, chunkAAD(id, ref))
	if err != nil {
		return nil, err
	}
	if len(plaintext) != ref.PlainSize {
		return nil, errors.New("stored-transfer chunk has the wrong plaintext length")
	}
	return plaintext, nil
}

// ChunkCipher reuses the derived data key and AEAD across chunks in one
// transfer. It also supports caller-owned output buffers for the hot path.
type ChunkCipher struct {
	aead cipher.AEAD
}

// NewChunkCipher prepares the data cipher for a stored transfer.
func NewChunkCipher(master []byte) (*ChunkCipher, error) {
	aead, err := aeadFor(master, "data")
	if err != nil {
		return nil, err
	}
	return &ChunkCipher{aead: aead}, nil
}

// NonceSize returns the prefix reserved before plaintext for SealInPlace.
func (c *ChunkCipher) NonceSize() int {
	return c.aead.NonceSize()
}

// Seal encrypts a chunk, reusing dst when it has sufficient capacity.
func (c *ChunkCipher) Seal(dst []byte, id string, ref ChunkRef, plaintext []byte) ([]byte, error) {
	if err := validateChunkRef(ref, len(plaintext)); err != nil {
		return nil, err
	}
	nonceSize := c.aead.NonceSize()
	needed := nonceSize + len(plaintext) + c.aead.Overhead()
	if cap(dst) < needed {
		dst = make([]byte, needed)
	} else {
		dst = dst[:needed]
	}
	nonce := dst[:nonceSize]
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate stored-transfer nonce: %w", err)
	}
	sealed := c.aead.Seal(dst[:nonceSize], nonce, plaintext, chunkAAD(id, ref))
	return sealed, nil
}

// SealInPlace encrypts plaintext already stored immediately after the nonce
// prefix in buffer. The ciphertext overwrites that plaintext.
func (c *ChunkCipher) SealInPlace(buffer []byte, id string, ref ChunkRef) ([]byte, error) {
	if err := validateChunkRef(ref, ref.PlainSize); err != nil {
		return nil, err
	}
	nonceSize := c.aead.NonceSize()
	required := nonceSize + ref.PlainSize + c.aead.Overhead()
	if len(buffer) < required {
		return nil, errors.New("stored-transfer chunk buffer is too small")
	}
	nonce := buffer[:nonceSize]
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate stored-transfer nonce: %w", err)
	}
	plaintext := buffer[nonceSize : nonceSize+ref.PlainSize]
	return c.aead.Seal(buffer[:nonceSize], nonce, plaintext, chunkAAD(id, ref)), nil
}

// Open authenticates and decrypts a chunk, reusing dst when possible.
func (c *ChunkCipher) Open(dst []byte, id string, ref ChunkRef, ciphertext []byte) ([]byte, error) {
	if err := validateChunkRef(ref, ref.PlainSize); err != nil {
		return nil, err
	}
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize+c.aead.Overhead() {
		return nil, errors.New("stored-transfer ciphertext is too short")
	}
	plaintext, err := c.aead.Open(
		dst[:0],
		ciphertext[:nonceSize],
		ciphertext[nonceSize:],
		chunkAAD(id, ref),
	)
	if err != nil {
		return nil, errors.New("stored-transfer authentication failed")
	}
	if len(plaintext) != ref.PlainSize {
		return nil, errors.New("stored-transfer chunk has the wrong plaintext length")
	}
	return plaintext, nil
}

// OpenInPlace authenticates and decrypts into ciphertext's payload area.
func (c *ChunkCipher) OpenInPlace(id string, ref ChunkRef, ciphertext []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize+c.aead.Overhead() {
		return nil, errors.New("stored-transfer ciphertext is too short")
	}
	return c.Open(ciphertext[nonceSize:nonceSize], id, ref, ciphertext)
}

func validateChunkRef(ref ChunkRef, plaintextLength int) error {
	if ref.ObjectIndex < 0 || ref.FileIndex < 0 || ref.FileChunk < 0 {
		return errors.New("stored-transfer chunk index cannot be negative")
	}
	if ref.PlainSize < 1 || ref.PlainSize > ChunkSize || plaintextLength != ref.PlainSize {
		return errors.New("stored-transfer chunk has an invalid plaintext length")
	}
	return nil
}

// ValidateManifest rejects malformed or unsafe metadata before any files are
// created by a receiver.
func ValidateManifest(manifest Manifest, maxBytes int64) error {
	if manifest.Version != Version {
		return fmt.Errorf("unsupported stored-transfer version %d", manifest.Version)
	}
	if manifest.ChunkSize != ChunkSize {
		return fmt.Errorf("unsupported stored-transfer chunk size %d", manifest.ChunkSize)
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > MaxFiles {
		return fmt.Errorf("stored transfer must contain between 1 and %d files", MaxFiles)
	}
	if maxBytes <= 0 {
		return errors.New("stored-transfer byte limit must be positive")
	}

	names := make(map[string]struct{}, len(manifest.Files))
	var total int64
	nextChunk := 0
	for index, file := range manifest.Files {
		if file.Name == "" || file.Name != path.Base(file.Name) ||
			strings.ContainsAny(file.Name, "/\\\x00") || file.Name == "." || file.Name == ".." {
			return fmt.Errorf("stored-transfer file %d has an unsafe name", index)
		}
		if _, exists := names[file.Name]; exists {
			return fmt.Errorf("duplicate stored-transfer filename %q", file.Name)
		}
		names[file.Name] = struct{}{}
		if file.Size < 0 || file.Size > maxBytes-total {
			return errors.New("stored transfer exceeds the configured byte limit")
		}
		total += file.Size
		hash, err := rawURL.DecodeString(file.SHA256)
		if err != nil || len(hash) != sha256.Size {
			return fmt.Errorf("stored-transfer file %q has an invalid SHA-256 hash", file.Name)
		}
		expectedChunks := int((file.Size + ChunkSize - 1) / ChunkSize)
		if file.FirstChunk != nextChunk || file.ChunkCount != expectedChunks {
			return fmt.Errorf("stored-transfer file %q has an invalid chunk map", file.Name)
		}
		nextChunk += expectedChunks
	}
	return nil
}

// ChunkRefs returns the authenticated context for every manifest chunk.
func ChunkRefs(manifest Manifest) []ChunkRef {
	var refs []ChunkRef
	for fileIndex, file := range manifest.Files {
		remaining := file.Size
		for fileChunk := 0; fileChunk < file.ChunkCount; fileChunk++ {
			size := ChunkSize
			if remaining < int64(size) {
				size = int(remaining)
			}
			refs = append(refs, ChunkRef{
				ObjectIndex: file.FirstChunk + fileChunk,
				FileIndex:   fileIndex,
				FileChunk:   fileChunk,
				PlainSize:   size,
			})
			remaining -= int64(size)
		}
	}
	return refs
}

// BrowserURL formats a share as a browser-safe fragment URL.
func (share Share) BrowserURL() (string, error) {
	origin, err := normalizeOrigin(share.Origin)
	if err != nil {
		return "", err
	}
	if err = validateShare(share); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/s/%s#v1.%s", origin, share.ID, rawURL.EncodeToString(share.MasterKey)), nil
}

// CLIToken formats a share as a shell-prompt-friendly token.
func (share Share) CLIToken() (string, error) {
	origin, err := normalizeOrigin(share.Origin)
	if err != nil {
		return "", err
	}
	if err = validateShare(share); err != nil {
		return "", err
	}
	return strings.Join([]string{
		Protocol,
		rawURL.EncodeToString([]byte(origin)),
		share.ID,
		rawURL.EncodeToString(share.MasterKey),
	}, "."), nil
}

// ParseShare parses either the canonical browser URL or the CLI token.
func ParseShare(value string) (Share, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, Protocol+".") {
		parts := strings.Split(value, ".")
		if len(parts) != 4 {
			return Share{}, errors.New("invalid stored-transfer token")
		}
		originBytes, err := rawURL.DecodeString(parts[1])
		if err != nil {
			return Share{}, errors.New("invalid stored-transfer origin")
		}
		key, err := rawURL.DecodeString(parts[3])
		if err != nil {
			return Share{}, errors.New("invalid stored-transfer key")
		}
		share := Share{Origin: string(originBytes), ID: parts[2], MasterKey: key}
		share.Origin, err = normalizeOrigin(share.Origin)
		if err != nil {
			return Share{}, err
		}
		if err = validateShare(share); err != nil {
			return Share{}, err
		}
		return share, nil
	}

	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme == "" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" {
		return Share{}, errors.New("invalid stored-transfer URL")
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) != 2 || segments[0] != "s" {
		return Share{}, errors.New("invalid stored-transfer URL path")
	}
	fragment := strings.TrimPrefix(parsed.Fragment, "v1.")
	if fragment == parsed.Fragment {
		return Share{}, errors.New("stored-transfer URL is missing a v1 key")
	}
	key, err := rawURL.DecodeString(fragment)
	if err != nil {
		return Share{}, errors.New("invalid stored-transfer URL key")
	}
	share := Share{
		Origin:    parsed.Scheme + "://" + parsed.Host,
		ID:        segments[1],
		MasterKey: key,
	}
	share.Origin, err = normalizeOrigin(share.Origin)
	if err != nil {
		return Share{}, err
	}
	if err = validateShare(share); err != nil {
		return Share{}, err
	}
	return share, nil
}

func normalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("stored-transfer origin must contain only scheme and host")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errors.New("stored-transfer origin must use HTTPS except on loopback")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func validateShare(share Share) error {
	id, err := rawURL.DecodeString(share.ID)
	if err != nil || len(id) != TransferIDLen || rawURL.EncodeToString(id) != share.ID {
		return errors.New("invalid stored-transfer id")
	}
	if len(share.MasterKey) != KeySize {
		return errors.New("invalid stored-transfer key length")
	}
	return nil
}

// EncodedSHA256 returns an unpadded URL-safe digest.
func EncodedSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return rawURL.EncodeToString(sum[:])
}

// DecodeBase64URL decodes a protocol byte string.
func DecodeBase64URL(value string) ([]byte, error) {
	return rawURL.DecodeString(value)
}

// EncodeBase64URL encodes a protocol byte string.
func EncodeBase64URL(value []byte) string {
	return rawURL.EncodeToString(value)
}
