package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v10/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Time() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func newTestService(t *testing.T, clock *testClock) *Service {
	t.Helper()
	service, err := New(Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 1 << 20,
		MaxTotalBytes:    4 << 20,
		MinFreeBytes:     1,
		MaxFiles:         10,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	return service
}

func request(t *testing.T, service *Service, method, target, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil && (method == http.MethodPut) {
		req.ContentLength = int64(len(body))
		req.Header.Set("X-Croc-SHA256", storecrypto.EncodedSHA256(body))
	}
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, req)
	return recorder
}

type storedFixture struct {
	id          string
	key         []byte
	redeemToken string
	uploadToken string
	manifest    []byte
	chunk       []byte
}

func createFixture(t *testing.T, service *Service) storedFixture {
	t.Helper()
	key, err := storecrypto.GenerateKey()
	require.NoError(t, err)
	redeem, err := storecrypto.RedeemCapability(key)
	require.NoError(t, err)
	fileHash := sha256.Sum256([]byte("hello"))
	manifest := storecrypto.Manifest{
		Version:   storecrypto.Version,
		ChunkSize: storecrypto.ChunkSize,
		Files: []storecrypto.ManifestFile{{
			Name:       "hello.txt",
			Size:       5,
			Modified:   time.Unix(1_700_000_000, 0).UTC(),
			SHA256:     storecrypto.EncodeBase64URL(fileHash[:]),
			FirstChunk: 0,
			ChunkCount: 1,
		}},
	}
	// Ciphertext AAD contains the server-generated ID, so declare lengths,
	// create the object, then encrypt and upload.
	manifestPlain, err := json.Marshal(manifest)
	require.NoError(t, err)
	input := createRequest{
		Protocol:       storecrypto.Protocol,
		ManifestBytes:  int64(len(manifestPlain) + 28),
		ChunkBytes:     []int64{5 + 28},
		RedeemVerifier: storecrypto.EncodeBase64URL(storecrypto.CapabilityVerifier(redeem)),
		DeclaredFiles:  1,
		PlaintextBytes: 5,
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	recorder := request(t, service, http.MethodPost, "/api/v1/store/transfers", "", body)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	var created createResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &created))

	encryptedManifest, err := storecrypto.SealManifest(key, created.ID, manifest)
	require.NoError(t, err)
	chunk, err := storecrypto.SealChunk(key, created.ID, storecrypto.ChunkRefs(manifest)[0], []byte("hello"))
	require.NoError(t, err)
	require.Equal(t, int(input.ManifestBytes), len(encryptedManifest))
	require.Equal(t, int(input.ChunkBytes[0]), len(chunk))

	recorder = request(t, service, http.MethodPut,
		fmt.Sprintf("/api/v1/store/transfers/%s/manifest", created.ID),
		created.UploadToken, encryptedManifest)
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	recorder = request(t, service, http.MethodPut,
		fmt.Sprintf("/api/v1/store/transfers/%s/chunks/0", created.ID),
		created.UploadToken, chunk)
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/complete", created.ID),
		created.UploadToken, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	return storedFixture{
		id:          created.ID,
		key:         key,
		redeemToken: storecrypto.EncodeBase64URL(redeem),
		uploadToken: created.UploadToken,
		manifest:    encryptedManifest,
		chunk:       chunk,
	}
}

func TestStoredTransferLifecycle(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	recorder := request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/manifest", fixture.id),
		storecrypto.EncodeBase64URL(make([]byte, 32)), nil)
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/manifest", fixture.id),
		fixture.redeemToken, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, fixture.manifest, recorder.Body.Bytes())

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	require.Equal(t, http.StatusCreated, recorder.Code)
	var claim claimResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &claim))

	locked := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusLocked, locked.Code)

	recorder = request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/chunks/0", fixture.id),
		claim.ClaimToken, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	plaintext, err := storecrypto.OpenChunk(
		fixture.key,
		fixture.id,
		storecrypto.ChunkRef{ObjectIndex: 0, FileIndex: 0, FileChunk: 0, PlainSize: 5},
		recorder.Body.Bytes(),
	)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), plaintext)

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/commit", fixture.id),
		claim.ClaimToken, nil)
	assert.Equal(t, http.StatusNoContent, recorder.Code)

	require.NoError(t, os.WriteFile(service.manifestPath(fixture.id), fixture.manifest, 0o600))
	require.NoError(t, os.MkdirAll(
		filepath.Dir(service.chunkPath(fixture.id, 0)),
		0o700,
	))
	require.NoError(t, os.WriteFile(service.chunkPath(fixture.id, 0), fixture.chunk, 0o600))
	require.NoError(t, service.Sweep())
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	assert.NoDirExists(t, filepath.Dir(service.chunkPath(fixture.id, 0)))

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusGone, recorder.Code)

	// A lost commit response can be retried with the same claim capability.
	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/commit", fixture.id),
		claim.ClaimToken, nil)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestClaimReleaseAndExpiry(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	recorder := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	require.Equal(t, http.StatusCreated, recorder.Code)
	var claim claimResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &claim))

	recorder = request(t, service, http.MethodDelete,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		claim.ClaimToken, nil)
	assert.Equal(t, http.StatusNoContent, recorder.Code)

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	require.Equal(t, http.StatusCreated, recorder.Code)
	var secondClaim claimResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &secondClaim))

	clock.Add(claimLifetime + time.Second)
	recorder = request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/chunks/0", fixture.id),
		secondClaim.ClaimToken, nil)
	assert.Equal(t, http.StatusGone, recorder.Code)

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/commit", fixture.id),
		secondClaim.ClaimToken, nil)
	assert.Equal(t, http.StatusGone, recorder.Code)

	recorder = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusCreated, recorder.Code)

	clock.Add(24*time.Hour + time.Second)
	require.NoError(t, service.Sweep())
	recorder = request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/manifest", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusGone, recorder.Code)
}

func TestSenderCanRevoke(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	recorder := request(t, service, http.MethodDelete,
		fmt.Sprintf("/api/v1/store/transfers/%s", fixture.id),
		fixture.uploadToken, nil)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
	recorder = request(t, service, http.MethodGet,
		fmt.Sprintf("/api/v1/store/transfers/%s/manifest", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusGone, recorder.Code)
}

func TestCrossOriginRequestsAreRejectedBeforeCreation(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/store/transfers", bytes.NewReader(nil))
	req.Header.Set("Origin", "https://attacker.example")
	recorder := httptest.NewRecorder()

	service.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, service.creationWindows)
}

func TestUnknownTransferIDsUseBoundedLockState(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	for index := 0; index < transferLockStripes*2; index++ {
		var raw [storecrypto.TransferIDLen]byte
		binary.BigEndian.PutUint64(raw[8:], uint64(index))
		id := storecrypto.EncodeBase64URL(raw[:])
		response := request(t, service, http.MethodGet,
			fmt.Sprintf("/api/v1/store/transfers/%s/manifest", id), "", nil)
		assert.Equal(t, http.StatusNotFound, response.Code)
	}

	assert.Len(t, service.transferLocks, transferLockStripes)
}

func TestStoreRootHasAnExclusiveProcessLock(t *testing.T) {
	root := t.TempDir()
	first, err := New(Config{Root: root, MinFreeBytes: 1})
	require.NoError(t, err)

	_, err = New(Config{Root: root, MinFreeBytes: 1})
	assert.ErrorContains(t, err, "already in use")
	require.NoError(t, first.Close())

	reopened, err := New(Config{Root: root, MinFreeBytes: 1})
	require.NoError(t, err)
	require.NoError(t, reopened.Close())
}

func TestRejectsTransferLimitAboveProtocolObjectCap(t *testing.T) {
	_, err := New(Config{
		Root:             t.TempDir(),
		MaxTransferBytes: int64(MaxChunkObjects)*storecrypto.ChunkSize + 1,
		MinFreeBytes:     1,
	})
	assert.ErrorContains(t, err, "byte limit")
}
