package storecrypto

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// These vectors are also consumed by the native browser crypto tests.
func TestBrowserV1Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/browser-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Key, ID, Redeem, ManifestJSON, ManifestCiphertext string
		Chunk                                             struct {
			ObjectIndex, FileIndex, FileChunk int64
			PlainSize                         int
			Plaintext, Ciphertext             string
		}
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	decode := func(value string) []byte {
		data, err := rawURL.DecodeString(value)
		require.NoError(t, err)
		return data
	}
	key := decode(fixture.Key)
	capability, err := RedeemCapability(key)
	require.NoError(t, err)
	require.Equal(t, decode(fixture.Redeem), capability)
	manifestCiphertext := decode(fixture.ManifestCiphertext)
	plaintext, err := OpenManifestJSON(key, fixture.ID, manifestCiphertext, 1024)
	require.NoError(t, err)
	require.Equal(t, fixture.ManifestJSON, string(plaintext))
	sealed, err := seal(key, "manifest", plaintext, manifestAAD(fixture.ID), bytes.NewReader(manifestCiphertext[:12]))
	require.NoError(t, err)
	require.Equal(t, manifestCiphertext, sealed)
	ref := ChunkRef{fixture.Chunk.ObjectIndex, fixture.Chunk.FileIndex, fixture.Chunk.FileChunk, fixture.Chunk.PlainSize}
	chunk := decode(fixture.Chunk.Ciphertext)
	opened, err := OpenChunk(key, fixture.ID, ref, chunk)
	require.NoError(t, err)
	require.Equal(t, decode(fixture.Chunk.Plaintext), opened)
	sealed, err = seal(key, "data", opened, chunkAAD(fixture.ID, ref), bytes.NewReader(chunk[:12]))
	require.NoError(t, err)
	require.Equal(t, chunk, sealed)
}

func TestBrowserManifestValidationFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/manifest-validation.json")
	require.NoError(t, err)
	var cases []struct {
		Name, Manifest string
		Valid          bool
		MaxBytes       int64
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			_, err := decodeManifest([]byte(test.Manifest), test.MaxBytes)
			if test.Valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
