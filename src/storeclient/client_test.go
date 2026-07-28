package storeclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schollz/croc/v10/src/store"
	"github.com/schollz/croc/v10/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStack(t *testing.T) (*Client, string) {
	t.Helper()
	service, err := store.New(store.Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 8 << 20,
		MaxTotalBytes:    32 << 20,
		MinFreeBytes:     1,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	server := httptest.NewServer(service)
	t.Cleanup(func() {
		server.Close()
		require.NoError(t, service.Close())
	})
	return &Client{HTTP: server.Client()}, server.URL
}

func TestUploadInspectReceiveAndConsume(t *testing.T) {
	client, origin := testStack(t)
	input := t.TempDir()
	alpha := filepath.Join(input, "alpha.bin")
	empty := filepath.Join(input, "empty.txt")
	alphaBytes := make([]byte, 2<<20+137)
	for index := range alphaBytes {
		alphaBytes[index] = byte(index % 251)
	}
	require.NoError(t, os.WriteFile(alpha, alphaBytes, 0o600))
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	result, err := client.Upload(context.Background(), origin, []string{alpha, empty}, Callbacks{})
	require.NoError(t, err)
	assert.False(t, result.ExpiresAt.IsZero())

	manifest, _, err := client.Inspect(context.Background(), result.Share)
	require.NoError(t, err)
	require.Len(t, manifest.Files, 2)
	assert.Equal(t, "alpha.bin", manifest.Files[0].Name)

	output := t.TempDir()
	require.NoError(t, client.Receive(
		context.Background(),
		result.Share,
		manifest,
		output,
		Callbacks{},
	))
	received, err := os.ReadFile(filepath.Join(output, "alpha.bin"))
	require.NoError(t, err)
	assert.Equal(t, alphaBytes, received)
	info, err := os.Stat(filepath.Join(output, "empty.txt"))
	require.NoError(t, err)
	assert.Zero(t, info.Size())

	_, _, err = client.Inspect(context.Background(), result.Share)
	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusGone, httpErr.StatusCode)
}

func TestSenderRevocation(t *testing.T) {
	client, origin := testStack(t)
	file := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(file, []byte("secret"), 0o600))
	result, err := client.Upload(context.Background(), origin, []string{file}, Callbacks{})
	require.NoError(t, err)
	require.NoError(t, client.Revoke(
		context.Background(),
		result.Share,
		result.UploadToken,
	))

	_, _, err = client.Inspect(context.Background(), result.Share)
	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusGone, httpErr.StatusCode)
}

func TestReceiveRenewsAnExpiredPersistedClaim(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	service, err := store.New(store.Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 8 << 20,
		MaxTotalBytes:    32 << 20,
		MinFreeBytes:     1,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              func() time.Time { return now },
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	server := httptest.NewServer(service)
	t.Cleanup(func() {
		server.Close()
		require.NoError(t, service.Close())
	})
	client := &Client{HTTP: server.Client()}
	input := filepath.Join(t.TempDir(), "resume.txt")
	require.NoError(t, os.WriteFile(input, []byte("resume me"), 0o600))
	result, err := client.Upload(context.Background(), server.URL, []string{input}, Callbacks{})
	require.NoError(t, err)
	manifest, _, err := client.Inspect(context.Background(), result.Share)
	require.NoError(t, err)
	staleClaim, err := client.claim(context.Background(), result.Share)
	require.NoError(t, err)

	output := t.TempDir()
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, writeState(
		filepath.Join(output, ".croc-store-"+result.Share.ID+".json"),
		downloadState{
			Version:      storecrypto.Version,
			ID:           result.Share.ID,
			ManifestHash: storecrypto.EncodedSHA256(manifestBytes),
			ClaimToken:   staleClaim,
			Completed:    make(map[int]bool),
			Renamed:      make(map[string]bool),
		},
	))
	now = now.Add(31 * time.Minute)

	require.NoError(t, client.Receive(
		context.Background(),
		result.Share,
		manifest,
		output,
		Callbacks{},
	))
	assert.Equal(t, []byte("resume me"), mustReadFile(t, filepath.Join(output, "resume.txt")))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	return bytes
}
