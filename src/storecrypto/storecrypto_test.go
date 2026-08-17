package storecrypto

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testManifest() Manifest {
	first := sha256.Sum256([]byte("hello"))
	second := sha256.Sum256(nil)
	return Manifest{
		Version:   Version,
		ChunkSize: ChunkSize,
		Files: []ManifestFile{
			{
				Name:       "hello.txt",
				Size:       5,
				Modified:   time.Unix(1_700_000_000, 0).UTC(),
				SHA256:     EncodeBase64URL(first[:]),
				FirstChunk: 0,
				ChunkCount: 1,
			},
			{
				Name:       "empty.bin",
				Size:       0,
				Modified:   time.Unix(1_700_000_001, 0).UTC(),
				SHA256:     EncodeBase64URL(second[:]),
				FirstChunk: 1,
				ChunkCount: 0,
			},
		},
	}
}

func TestManifestAndChunkRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeySize)
	id := EncodeBase64URL(bytes.Repeat([]byte{9}, TransferIDLen))
	manifest := testManifest()

	encryptedManifest, err := SealManifest(key, id, manifest)
	require.NoError(t, err)
	opened, err := OpenManifest(key, id, encryptedManifest, 1024)
	require.NoError(t, err)
	assert.Equal(t, manifest, opened)

	ref := ChunkRefs(manifest)[0]
	encryptedChunk, err := SealChunk(key, id, ref, []byte("hello"))
	require.NoError(t, err)
	plaintext, err := OpenChunk(key, id, ref, encryptedChunk)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), plaintext)

	chunkCipher, err := NewChunkCipher(key)
	require.NoError(t, err)
	buffer := make([]byte, chunkCipher.NonceSize()+ref.PlainSize+16)
	copy(buffer[chunkCipher.NonceSize():], "hello")
	inPlaceCiphertext, err := chunkCipher.SealInPlace(buffer, id, ref)
	require.NoError(t, err)
	inPlacePlaintext, err := chunkCipher.OpenInPlace(id, ref, inPlaceCiphertext)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), inPlacePlaintext)

	encryptedChunk[len(encryptedChunk)-1] ^= 1
	_, err = OpenChunk(key, id, ref, encryptedChunk)
	assert.ErrorContains(t, err, "authentication failed")
}

func TestChunkContextPreventsReordering(t *testing.T) {
	key := bytes.Repeat([]byte{1}, KeySize)
	id := EncodeBase64URL(bytes.Repeat([]byte{2}, TransferIDLen))
	ref := ChunkRef{ObjectIndex: 0, FileIndex: 0, FileChunk: 0, PlainSize: 3}
	ciphertext, err := SealChunk(key, id, ref, []byte("abc"))
	require.NoError(t, err)

	ref.ObjectIndex = 1
	_, err = OpenChunk(key, id, ref, ciphertext)
	assert.ErrorContains(t, err, "authentication failed")
}

func TestShareURLAndTokenRoundTrip(t *testing.T) {
	share := Share{
		Origin:    "https://files.example.test",
		ID:        EncodeBase64URL(bytes.Repeat([]byte{3}, TransferIDLen)),
		MasterKey: bytes.Repeat([]byte{4}, KeySize),
	}
	browserURL, err := share.BrowserURL()
	require.NoError(t, err)
	token, err := share.CLIToken()
	require.NoError(t, err)

	fromURL, err := ParseShare(browserURL)
	require.NoError(t, err)
	assert.Equal(t, share, fromURL)
	fromToken, err := ParseShare(token)
	require.NoError(t, err)
	assert.Equal(t, share, fromToken)

	_, err = ParseShare(browserURL[:strings.Index(browserURL, "#")] + "?tracking=1" + browserURL[strings.Index(browserURL, "#"):])
	assert.ErrorContains(t, err, "invalid stored-transfer URL")
}

func TestValidateManifestRejectsUnsafeMetadata(t *testing.T) {
	manifest := testManifest()
	manifest.Files[0].Name = "../secret"
	assert.ErrorContains(t, ValidateManifest(manifest, 1024), "unsafe name")

	manifest = testManifest()
	manifest.Files[0].FirstChunk = 4
	assert.ErrorContains(t, ValidateManifest(manifest, 1024), "invalid chunk map")
}
