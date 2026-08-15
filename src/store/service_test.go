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

	"github.com/schollz/croc/v11/src/storecrypto"
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
	return newTestServiceWithMaxExpiration(t, clock, 0)
}

func newTestServiceWithMaxExpiration(t *testing.T, clock *testClock, maxExpiration time.Duration) *Service {
	t.Helper()
	service, err := New(Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 1 << 20,
		MaxTotalBytes:    4 << 20,
		MinFreeBytes:     1,
		MaxFiles:         10,
		MaxDownloads:     10,
		MaxExpiration:    maxExpiration,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	return service
}

func TestStoredExpirationDefaultCustomAndServerMaximum(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()

	t.Run("one-day default with unlimited maximum", func(t *testing.T) {
		clock := &testClock{now: start}
		service := newTestService(t, clock)
		fixture := createFixture(t, service)
		assert.Equal(t, start.Add(DefaultExpiration), fixture.expiresAt)
		assert.Equal(t, int64(DefaultExpiration/time.Second), service.PublicConfig().ExpiresSeconds)
		assert.Zero(t, service.PublicConfig().MaxExpiresSeconds)
	})

	t.Run("custom lifetime", func(t *testing.T) {
		clock := &testClock{now: start}
		service := newTestService(t, clock)
		seconds := int64((3 * 24 * time.Hour) / time.Second)
		fixture := createUploadingFixtureOptions(t, service, 0, &seconds)
		clock.Add(30 * time.Minute)
		response := request(t, service, http.MethodPost,
			fmt.Sprintf("/api/v1/store/transfers/%s/complete", fixture.id),
			fixture.uploadToken, nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var completed completeResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &completed))
		assert.Equal(t, start.Add(30*time.Minute+3*24*time.Hour), completed.ExpiresAt)
		meta, err := service.load(fixture.id)
		require.NoError(t, err)
		assert.Equal(t, seconds, meta.ExpiresSeconds)
	})

	t.Run("request is silently clamped", func(t *testing.T) {
		clock := &testClock{now: start}
		service := newTestServiceWithMaxExpiration(t, clock, 36*time.Hour)
		seconds := int64((3 * 24 * time.Hour) / time.Second)
		fixture := createFixtureOptions(t, service, 0, &seconds)
		assert.Equal(t, start.Add(36*time.Hour), fixture.expiresAt)
		meta, err := service.load(fixture.id)
		require.NoError(t, err)
		assert.Equal(t, int64((36*time.Hour)/time.Second), meta.ExpiresSeconds)
	})

	t.Run("maximum below one day becomes the default", func(t *testing.T) {
		clock := &testClock{now: start}
		service := newTestServiceWithMaxExpiration(t, clock, 12*time.Hour)
		fixture := createFixture(t, service)
		assert.Equal(t, start.Add(12*time.Hour), fixture.expiresAt)
		public := service.PublicConfig()
		assert.Equal(t, int64((12*time.Hour)/time.Second), public.ExpiresSeconds)
		assert.Equal(t, int64((12*time.Hour)/time.Second), public.MaxExpiresSeconds)
	})
}

func TestStoredExpirationDeclarationValidation(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	redeem := storecrypto.EncodeBase64URL(make([]byte, sha256.Size))

	for _, seconds := range []int64{0, -1, 59, MaxExpirationSeconds + 1} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			body, err := json.Marshal(createRequest{
				Protocol:       storecrypto.Protocol,
				ManifestBytes:  29,
				RedeemVerifier: redeem,
				DeclaredFiles:  1,
				ExpiresSeconds: &seconds,
			})
			require.NoError(t, err)
			response := request(t, service, http.MethodPost, "/api/v1/store/transfers", "", body)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

func TestExpirationDoesNotChangeCompletionResponseJSON(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	seconds := int64((2 * 24 * time.Hour) / time.Second)
	fixture := createFixtureOptions(t, service, 0, &seconds)

	response := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/complete", fixture.id),
		fixture.uploadToken, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var completed map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &completed))
	assert.Equal(t, map[string]any{"expiresAt": fixture.expiresAt.Format(time.RFC3339)}, completed)
}

func TestRejectsInvalidServerMaximumExpiration(t *testing.T) {
	for _, maximum := range []time.Duration{-time.Minute, 30 * time.Second} {
		_, err := New(Config{Root: t.TempDir(), MinFreeBytes: 1, MaxExpiration: maximum})
		assert.ErrorContains(t, err, "maximum expiration")
	}
}

func TestCustomExpirationCleanup(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	seconds := int64((90 * time.Minute) / time.Second)
	fixture := createFixtureOptions(t, service, 0, &seconds)

	clock.Add(90 * time.Minute)
	require.NoError(t, service.Sweep())
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
}

func TestAcceptedExpirationSurvivesRestartAndMaximumChange(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	root := t.TempDir()
	config := Config{
		Root:             root,
		MaxTransferBytes: 1 << 20,
		MaxTotalBytes:    4 << 20,
		MinFreeBytes:     1,
		MaxFiles:         10,
		MaxDownloads:     10,
		MaxExpiration:    7 * 24 * time.Hour,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	}
	service, err := New(config)
	require.NoError(t, err)
	seconds := int64((5 * 24 * time.Hour) / time.Second)
	fixture := createUploadingFixtureOptions(t, service, 0, &seconds)
	require.NoError(t, service.Close())

	config.MaxExpiration = 24 * time.Hour
	reopened, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	meta, err := reopened.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, seconds, meta.ExpiresSeconds)
	assert.Equal(t, stateUploading, meta.State)
	response := request(t, reopened, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/complete", fixture.id),
		fixture.uploadToken, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var completed completeResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &completed))
	assert.Equal(t, clock.Time().Add(5*24*time.Hour), completed.ExpiresAt)
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
	expiresAt   time.Time
}

func createFixture(t *testing.T, service *Service) storedFixture {
	return createFixtureDownloads(t, service, 0)
}

func createFixtureDownloads(t *testing.T, service *Service, downloads int) storedFixture {
	return createFixtureOptions(t, service, downloads, nil)
}

func createFixtureOptions(t *testing.T, service *Service, downloads int, expiresSeconds *int64) storedFixture {
	t.Helper()
	fixture := createUploadingFixtureOptions(t, service, downloads, expiresSeconds)
	recorder := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/complete", fixture.id),
		fixture.uploadToken, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var completed completeResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &completed))
	fixture.expiresAt = completed.ExpiresAt
	return fixture
}

func createUploadingFixtureOptions(t *testing.T, service *Service, downloads int, expiresSeconds *int64) storedFixture {
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
		ExpiresSeconds: expiresSeconds,
	}
	if downloads > 0 {
		input.Downloads = &downloads
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
	return storedFixture{
		id:          created.ID,
		key:         key,
		redeemToken: storecrypto.EncodeBase64URL(redeem),
		uploadToken: created.UploadToken,
		manifest:    encryptedManifest,
		chunk:       chunk,
	}
}

func claimTransfer(t *testing.T, service *Service, fixture storedFixture) claimResponse {
	t.Helper()
	recorder := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	var claim claimResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &claim))
	return claim
}

func commitTransfer(t *testing.T, service *Service, fixture storedFixture, token string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/commit", fixture.id),
		token, nil)
}

func TestMultipleStoredDownloadsAreCountedAndCommittedIdempotently(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixtureDownloads(t, service, 3)

	first := claimTransfer(t, service, fixture)
	response := commitTransfer(t, service, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "2", response.Header().Get("X-Croc-Downloads-Remaining"))
	assert.FileExists(t, service.manifestPath(fixture.id))
	assert.FileExists(t, service.chunkPath(fixture.id, 0))

	// A lost response may cause the same commit to be retried. It must not
	// consume another download.
	response = commitTransfer(t, service, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "2", response.Header().Get("X-Croc-Downloads-Remaining"))

	second := claimTransfer(t, service, fixture)
	// Retrying an older commit while another receiver owns the claim must not
	// release or otherwise disturb the active claim.
	response = commitTransfer(t, service, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "2", response.Header().Get("X-Croc-Downloads-Remaining"))
	response = commitTransfer(t, service, fixture, second.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "1", response.Header().Get("X-Croc-Downloads-Remaining"))

	// All committed claim verifiers remain idempotent, not only the latest one.
	response = commitTransfer(t, service, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "1", response.Header().Get("X-Croc-Downloads-Remaining"))

	third := claimTransfer(t, service, fixture)
	response = commitTransfer(t, service, fixture, third.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "0", response.Header().Get("X-Croc-Downloads-Remaining"))
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	assert.NoDirExists(t, filepath.Dir(service.chunkPath(fixture.id, 0)))

	response = commitTransfer(t, service, fixture, second.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "0", response.Header().Get("X-Croc-Downloads-Remaining"))
	response = request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/claim", fixture.id),
		fixture.redeemToken, nil)
	assert.Equal(t, http.StatusGone, response.Code)
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

func TestStoredDownloadDeclarationDefaultsAndLimits(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	redeem := storecrypto.EncodeBase64URL(make([]byte, sha256.Size))

	create := func(downloads *int) *httptest.ResponseRecorder {
		body, err := json.Marshal(createRequest{
			Protocol:       storecrypto.Protocol,
			ManifestBytes:  29,
			RedeemVerifier: redeem,
			DeclaredFiles:  1,
			PlaintextBytes: 0,
			Downloads:      downloads,
		})
		require.NoError(t, err)
		return request(t, service, http.MethodPost, "/api/v1/store/transfers", "", body)
	}

	response := create(nil)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	assert.Equal(t, "1", response.Header().Get("X-Croc-Downloads"))

	zero := 0
	response = create(&zero)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	overLimit := service.config.MaxDownloads + 1
	response = create(&overLimit)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestLegacyStoredMetadataDefaultsToOneDownload(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	contents, err := os.ReadFile(service.metadataPath(fixture.id))
	require.NoError(t, err)
	var legacy map[string]any
	require.NoError(t, json.Unmarshal(contents, &legacy))
	delete(legacy, "downloadsTotal")
	delete(legacy, "downloadsRemaining")
	legacyBytes, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(service.metadataPath(fixture.id), legacyBytes, 0o600))

	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, 1, meta.DownloadsTotal)
	assert.Equal(t, 1, meta.DownloadsRemaining)

	claim := claimTransfer(t, service, fixture)
	response := commitTransfer(t, service, fixture, claim.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "0", response.Header().Get("X-Croc-Downloads-Remaining"))
}

func TestLegacyUploadingMetadataDefaultsToOneDayExpiration(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	contents, err := os.ReadFile(service.metadataPath(fixture.id))
	require.NoError(t, err)
	var legacy map[string]any
	require.NoError(t, json.Unmarshal(contents, &legacy))
	legacy["state"] = string(stateUploading)
	delete(legacy, "completedAt")
	delete(legacy, "expiresAt")
	delete(legacy, "expiresSeconds")
	legacyBytes, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(service.metadataPath(fixture.id), legacyBytes, 0o600))

	response := request(t, service, http.MethodPost,
		fmt.Sprintf("/api/v1/store/transfers/%s/complete", fixture.id),
		fixture.uploadToken, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var completed completeResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &completed))
	assert.Equal(t, clock.Time().Add(DefaultExpiration), completed.ExpiresAt)
}

func TestRemainingDownloadsAndCommitHistorySurviveRestart(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	root := t.TempDir()
	config := Config{
		Root:             root,
		MaxTransferBytes: 1 << 20,
		MaxTotalBytes:    4 << 20,
		MinFreeBytes:     1,
		MaxFiles:         10,
		MaxDownloads:     10,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	}
	service, err := New(config)
	require.NoError(t, err)
	fixture := createFixtureDownloads(t, service, 2)
	first := claimTransfer(t, service, fixture)
	response := commitTransfer(t, service, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	require.NoError(t, service.Close())

	reopened, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	meta, err := reopened.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, 1, meta.DownloadsRemaining)
	assert.Len(t, meta.CommittedClaims, 1)

	response = commitTransfer(t, reopened, fixture, first.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "1", response.Header().Get("X-Croc-Downloads-Remaining"))
	second := claimTransfer(t, reopened, fixture)
	response = commitTransfer(t, reopened, fixture, second.ClaimToken)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Equal(t, "0", response.Header().Get("X-Croc-Downloads-Remaining"))
}

func TestConcurrentCommitRetriesDecrementOnce(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixtureDownloads(t, service, 2)
	claim := claimTransfer(t, service, fixture)
	target := fmt.Sprintf("/api/v1/store/transfers/%s/commit", fixture.id)

	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest(http.MethodPost, target, nil)
			req.Header.Set("Authorization", "Bearer "+claim.ClaimToken)
			recorder := httptest.NewRecorder()
			service.ServeHTTP(recorder, req)
			responses <- recorder
		}()
	}
	for range 2 {
		response := <-responses
		require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
		assert.Equal(t, "1", response.Header().Get("X-Croc-Downloads-Remaining"))
	}
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, 1, meta.DownloadsRemaining)
	assert.Len(t, meta.CommittedClaims, 1)
}
