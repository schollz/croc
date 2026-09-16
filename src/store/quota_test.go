package store

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServiceWithQuota(t *testing.T, clock *testClock, maxTransfer, maxTotal int64) *Service {
	t.Helper()
	service, err := New(Config{
		Root:             t.TempDir(),
		MaxTransferBytes: maxTransfer,
		MaxTotalBytes:    maxTotal,
		MinFreeBytes:     1,
		MaxFiles:         10,
		MaxDownloads:     10,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	return service
}

func TestReserveAccounting(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	require.True(t, service.reserve(1024))
	assert.Equal(t, int64(1024), service.reservedBytes)
	require.True(t, service.reserve(2048))
	assert.Equal(t, int64(3072), service.reservedBytes)

	service.unreserve(2048)
	assert.Equal(t, int64(1024), service.reservedBytes)
}

func TestReserveRejectsInvalidAndOverQuotaRequests(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	require.False(t, service.reserve(0))
	require.False(t, service.reserve(-1))
	assert.Zero(t, service.reservedBytes)

	quota := service.config.MaxTotalBytes
	require.True(t, service.reserve(1024))
	require.False(t, service.reserve(quota))
	require.False(t, service.reserve(quota-1024+1))
	require.True(t, service.reserve(quota-1024))
	assert.Equal(t, quota, service.reservedBytes)
	require.False(t, service.reserve(1))
}

func TestReserveRejectsWhenDiskSpaceIsInsufficient(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service, err := New(Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 1 << 20,
		MaxTotalBytes:    4 << 20,
		MinFreeBytes:     1 << 60,
		MaxFiles:         10,
		MaxDownloads:     10,
		CreatePerHour:    100,
		MaxActiveUploads: 10,
		Now:              clock.Time,
		DisableRootLock:  true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	require.False(t, service.reserve(1))
	assert.Zero(t, service.reservedBytes)
}

func TestUnreserveClampsAtZero(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	service.unreserve(100)
	assert.Zero(t, service.reservedBytes)

	require.True(t, service.reserve(50))
	service.unreserve(200)
	assert.Zero(t, service.reservedBytes)
}

func createDeclaredTransfer(t *testing.T, service *Service, plaintext int64) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	redeem := storecrypto.EncodeBase64URL(make([]byte, sha256.Size))
	body, err := json.Marshal(createRequest{
		Protocol:       storecrypto.Protocol,
		ManifestBytes:  29,
		ChunkBytes:     []int64{plaintext + 28},
		RedeemVerifier: redeem,
		DeclaredFiles:  1,
		PlaintextBytes: plaintext,
	})
	require.NoError(t, err)
	recorder := request(t, service, http.MethodPost, "/api/v1/store/transfers", "", body)
	if recorder.Code != http.StatusCreated {
		return recorder, "", ""
	}
	var created createResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &created))
	return recorder, created.ID, created.UploadToken
}

func TestStoredQuotaFillsAndRecoversAfterRevoke(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestServiceWithQuota(t, clock, 8<<20, 6<<20)
	const declared = int64(3 << 20)

	first, id, uploadToken := createDeclaredTransfer(t, service, declared)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	reservedAfterFirst := service.reservedBytes
	assert.True(t, reservedAfterFirst > declared)

	second, _, _ := createDeclaredTransfer(t, service, declared)
	require.Equal(t, http.StatusInsufficientStorage, second.Code, second.Body.String())
	assert.Equal(t, reservedAfterFirst, service.reservedBytes)

	recorder := request(t, service, http.MethodDelete,
		"/api/v1/store/transfers/"+id, uploadToken, nil)
	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())

	third, _, _ := createDeclaredTransfer(t, service, declared)
	require.Equal(t, http.StatusCreated, third.Code, third.Body.String())
}

func TestAbandonedUploadReleasesQuota(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createUploadingFixtureOptions(t, service, 0, nil)

	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	require.Equal(t, stateUploading, meta.State)
	require.Positive(t, meta.ReservedBytes)
	assert.Equal(t, meta.ReservedBytes, service.reservedBytes)

	clock.Add(uploadLifetime + time.Second)
	require.NoError(t, service.Sweep())

	meta, err = service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	assert.Zero(t, meta.ReservedBytes)
	assert.Zero(t, service.reservedBytes)
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	assert.NoDirExists(t, filepath.Dir(service.chunkPath(fixture.id, 0)))

	service.mu.Lock()
	totalActive := 0
	for _, active := range service.activeUploads {
		totalActive += active
	}
	service.mu.Unlock()
	assert.Zero(t, totalActive)
}

func TestExpiredTombstoneIsReclaimedBySweep(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)
	require.True(t, service.reservedBytes > 0)

	clock.Add(24*time.Hour + time.Second)
	require.NoError(t, service.Sweep())
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	assert.Zero(t, service.reservedBytes)
	assert.NoFileExists(t, service.manifestPath(fixture.id))

	clock.Add(tombstoneLifetime + time.Second)
	require.NoError(t, service.Sweep())
	assert.NoDirExists(t, service.transferDir(fixture.id))
	assert.NoFileExists(t, service.metadataPath(fixture.id))
	assert.Zero(t, service.reservedBytes)
}

func TestRemoveTransferReleasesReservedQuota(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	require.True(t, service.reserve(100))

	id, err := storecrypto.GenerateTransferID()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(service.transferDir(id), 0o700))
	require.NoError(t, os.WriteFile(service.metadataPath(id), []byte("x"), 0o600))

	require.NoError(t, service.removeTransfer(&metadata{ID: id, ReservedBytes: 40}))
	assert.NoDirExists(t, service.transferDir(id))
	assert.Equal(t, int64(60), service.reservedBytes)
}
