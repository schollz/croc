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

	"github.com/schollz/croc/v11/src/store"
	"github.com/schollz/croc/v11/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func BenchmarkStoredChunkUploadConcurrency(b *testing.B) {
	const chunks = 4
	path := filepath.Join(b.TempDir(), "upload.bin")
	handle, err := os.Create(path)
	require.NoError(b, err)
	require.NoError(b, handle.Truncate(chunks*storecrypto.ChunkSize))
	require.NoError(b, handle.Close())
	key, err := storecrypto.GenerateKey()
	require.NoError(b, err)
	refs := make([]storecrypto.ChunkRef, chunks)
	for index := range refs {
		refs[index] = storecrypto.ChunkRef{
			ObjectIndex: index, FileIndex: 0, FileChunk: index, PlainSize: storecrypto.ChunkSize,
		}
	}
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		time.Sleep(10 * time.Millisecond)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}}
	result := UploadResult{Share: storecrypto.Share{Origin: "https://store.test", ID: "benchmark", MasterKey: key}}
	file := uploadFile{path: path, manifest: storecrypto.ManifestFile{Name: "upload.bin", Size: chunks * storecrypto.ChunkSize, ChunkCount: chunks}}

	b.Run("sequential", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			handle, openErr := os.Open(path)
			if openErr != nil {
				b.Fatal(openErr)
			}
			chunkCipher, cipherErr := storecrypto.NewChunkCipher(key)
			if cipherErr != nil {
				b.Fatal(cipherErr)
			}
			plaintext := make([]byte, storecrypto.ChunkSize)
			ciphertext := make([]byte, 0, storecrypto.ChunkSize+32)
			for chunk, ref := range refs {
				if _, readErr := handle.ReadAt(plaintext, int64(chunk)*storecrypto.ChunkSize); readErr != nil {
					b.Fatal(readErr)
				}
				ciphertext, cipherErr = chunkCipher.Seal(ciphertext, result.Share.ID, ref, plaintext)
				if cipherErr != nil {
					b.Fatal(cipherErr)
				}
				if putErr := client.putWithRetry(context.Background(), "https://store.test/chunk", "", ciphertext); putErr != nil {
					b.Fatal(putErr)
				}
			}
			handle.Close()
		}
	})
	b.Run("four-workers", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			if _, uploadErr := client.uploadFile(context.Background(), result, file, 0, 1, refs, 0, file.manifest.Size, Callbacks{}); uploadErr != nil {
				b.Fatal(uploadErr)
			}
		}
	})
}

func testStack(t *testing.T) (*Client, string) {
	t.Helper()
	service, err := store.New(store.Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 8 << 20,
		MaxTotalBytes:    32 << 20,
		MinFreeBytes:     1,
		MaxDownloads:     10,
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

func TestUploadWithMultipleDownloadsRemainsAvailableUntilLastCommit(t *testing.T) {
	client, origin := testStack(t)
	file := filepath.Join(t.TempDir(), "shared.txt")
	require.NoError(t, os.WriteFile(file, []byte("shared three times"), 0o600))
	result, err := client.UploadWithDownloads(
		context.Background(),
		origin,
		[]string{file},
		3,
		Callbacks{},
	)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Downloads)

	for download := 0; download < 3; download++ {
		manifest, _, inspectErr := client.Inspect(context.Background(), result.Share)
		require.NoError(t, inspectErr)
		output := t.TempDir()
		require.NoError(t, client.Receive(
			context.Background(),
			result.Share,
			manifest,
			output,
			Callbacks{},
		))
		assert.Equal(t, []byte("shared three times"), mustReadFile(t, filepath.Join(output, "shared.txt")))
	}

	_, _, err = client.Inspect(context.Background(), result.Share)
	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusGone, httpErr.StatusCode)
}

func TestUploadRejectsNonPositiveDownloadCount(t *testing.T) {
	client, origin := testStack(t)
	_, err := client.UploadWithDownloads(
		context.Background(), origin, []string{"unused"}, 0, Callbacks{},
	)
	assert.ErrorContains(t, err, "must be positive")
}

func TestDefaultUploadOmitsDownloadFieldForOlderServers(t *testing.T) {
	var declaration map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.NoError(t, json.NewDecoder(request.Body).Decode(&declaration))
		response.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(response).Encode(createResponse{
			ID:              storecrypto.EncodeBase64URL(make([]byte, storecrypto.TransferIDLen)),
			UploadToken:     storecrypto.EncodeBase64URL(make([]byte, 32)),
			UploadExpiresAt: time.Now().Add(time.Hour),
			ChunkSize:       storecrypto.ChunkSize,
		}))
	}))
	t.Cleanup(server.Close)
	master, err := storecrypto.GenerateKey()
	require.NoError(t, err)
	result, err := (&Client{HTTP: server.Client()}).createUpload(
		context.Background(),
		server.URL,
		master,
		preparedUpload{manifestJSON: []byte("{}")},
		1,
		Callbacks{},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Downloads)
	_, declared := declaration["downloads"]
	assert.False(t, declared)
	_, declared = declaration["expiresSeconds"]
	assert.False(t, declared)
}

func TestUploadWithCustomExpirationUsesCompletionExpiration(t *testing.T) {
	client, origin := testStack(t)
	file := filepath.Join(t.TempDir(), "custom-expiration.txt")
	require.NoError(t, os.WriteFile(file, []byte("temporary"), 0o600))
	started := time.Now()
	result, err := client.UploadWithOptions(
		context.Background(),
		origin,
		[]string{file},
		UploadOptions{Downloads: 1, Expiration: 90 * time.Minute},
		Callbacks{},
	)
	require.NoError(t, err)
	assert.WithinDuration(t, started.Add(90*time.Minute), result.ExpiresAt, 5*time.Second)
}

func TestUploadRejectsExpirationBelowOneMinute(t *testing.T) {
	client, origin := testStack(t)
	_, err := client.UploadWithOptions(
		context.Background(), origin, []string{"unused"},
		UploadOptions{Downloads: 1, Expiration: 59 * time.Second}, Callbacks{},
	)
	assert.ErrorContains(t, err, "at least one minute")
}

func TestCustomExpirationFailsAgainstStrictOldServerButDefaultStillWorks(t *testing.T) {
	type oldCreateRequest struct {
		Protocol       string  `json:"protocol"`
		ManifestBytes  int64   `json:"manifestBytes"`
		ChunkBytes     []int64 `json:"chunkBytes"`
		RedeemVerifier string  `json:"redeemVerifier"`
		Files          int     `json:"files"`
		PlaintextBytes int64   `json:"plaintextBytes"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var declaration oldCreateRequest
		if err := decoder.Decode(&declaration); err != nil {
			http.Error(response, "invalid stored-transfer request", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(response).Encode(createResponse{
			ID:              storecrypto.EncodeBase64URL(make([]byte, storecrypto.TransferIDLen)),
			UploadToken:     storecrypto.EncodeBase64URL(make([]byte, 32)),
			UploadExpiresAt: time.Now().Add(time.Hour),
			ChunkSize:       storecrypto.ChunkSize,
		}))
	}))
	t.Cleanup(server.Close)
	client := &Client{HTTP: server.Client()}
	master, err := storecrypto.GenerateKey()
	require.NoError(t, err)
	prepared := preparedUpload{manifestJSON: []byte("{}")}

	_, err = client.createUploadWithOptions(
		context.Background(), server.URL, master, prepared,
		UploadOptions{Downloads: 1, Expiration: 2 * 24 * time.Hour}, Callbacks{},
	)
	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)

	_, err = client.createUpload(
		context.Background(), server.URL, master, prepared, 1, Callbacks{},
	)
	require.NoError(t, err)
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
