package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadState(t *testing.T, service *Service, id string) state {
	t.Helper()
	meta, err := service.load(id)
	require.NoError(t, err)
	return meta.State
}

func TestSweepReclaimsClaimedTransferWhenClaimExpires(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	claimTransfer(t, service, fixture)
	require.Equal(t, stateClaimed, loadState(t, service, fixture.id))

	// Claim expires, but transfer is still within its 24h lifetime.
	clock.Add(claimLifetime + time.Second)
	require.NoError(t, service.Sweep())

	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateAvailable, meta.State)
	assert.Empty(t, meta.ClaimVerifier)
	assert.True(t, meta.ClaimExpiresAt.IsZero())

	// Receiver can claim again after sweep reclaimed the expired claim.
	second := claimTransfer(t, service, fixture)
	assert.NotEmpty(t, second.ClaimToken)
}

func TestSweepDoesNotResetClaimedTransferIfClaimStillValid(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	claimTransfer(t, service, fixture)
	require.Equal(t, stateClaimed, loadState(t, service, fixture.id))

	// Advance time but keep claim alive (less than claimLifetime).
	clock.Add(claimLifetime / 2)
	require.NoError(t, service.Sweep())

	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateClaimed, meta.State)
	assert.NotEmpty(t, meta.ClaimVerifier)
}

func TestSweepExpiresAvailableTransfer(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	require.Equal(t, stateAvailable, loadState(t, service, fixture.id))

	// Advance past the 24h default expiration.
	clock.Add(24*time.Hour + time.Second)
	require.NoError(t, service.Sweep())

	assert.Equal(t, stateExpired, loadState(t, service, fixture.id))
	assert.NoFileExists(t, service.manifestPath(fixture.id))
}

func TestSweepPurgesCiphertextImmediatelyAndRemovesMetadataAfterTombstoneLifetime(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)

	// Expire the available transfer: ciphertext purged, tombstone metadata remains.
	clock.Add(24*time.Hour + time.Second)
	require.NoError(t, service.Sweep())
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	assert.True(t, meta.TombstoneExpiresAt.After(clock.Time()))

	// Tombstone not yet expired: directory and metadata still exist.
	clock.Add(tombstoneLifetime / 2)
	require.NoError(t, service.Sweep())
	assert.DirExists(t, service.transferDir(fixture.id))

	// Tombstone expires: entire transfer directory removed.
	clock.Add(tombstoneLifetime/2 + time.Second)
	require.NoError(t, service.Sweep())
	assert.NoDirExists(t, service.transferDir(fixture.id))
	assert.NoFileExists(t, service.metadataPath(fixture.id))
}

func TestSweepExpiredUploadThenTombstoneReclaim(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createUploadingFixtureOptions(t, service, 0, nil)

	require.Equal(t, stateUploading, loadState(t, service, fixture.id))
	require.True(t, service.reservedBytes > 0)

	// Upload expires: tombstone created, ciphertext purged, quota released.
	clock.Add(uploadLifetime + time.Second)
	require.NoError(t, service.Sweep())
	assert.Equal(t, stateExpired, loadState(t, service, fixture.id))
	assert.NoFileExists(t, service.manifestPath(fixture.id))
	assert.Zero(t, service.reservedBytes)

	// Tombstone expires later: directory fully removed.
	clock.Add(tombstoneLifetime + time.Second)
	require.NoError(t, service.Sweep())
	assert.NoDirExists(t, service.transferDir(fixture.id))
}

func TestSweepCleansMultipleTransfersWithDifferentLifetimes(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	shortSeconds := int64((2 * time.Hour) / time.Second)
	shortFixture := createFixtureOptions(t, service, 0, &shortSeconds)
	standardFixture := createFixture(t, service)
	require.NotEqual(t, shortFixture.id, standardFixture.id)

	// After 3 hours: short transfer expired, standard still available.
	clock.Add(3 * time.Hour)
	require.NoError(t, service.Sweep())
	assert.Equal(t, stateExpired, loadState(t, service, shortFixture.id))
	assert.Equal(t, stateAvailable, loadState(t, service, standardFixture.id))

	// After 25 hours: both expired.
	clock.Add(22 * time.Hour)
	require.NoError(t, service.Sweep())
	assert.Equal(t, stateExpired, loadState(t, service, shortFixture.id))
	assert.Equal(t, stateExpired, loadState(t, service, standardFixture.id))
}

func TestRunCleanupStopsWhenContextIsCancelled(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		service.RunCleanup(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCleanup did not return after context cancellation")
	}
}

func TestRunCleanupSweepsExpiredTransfers(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	service.config.CleanupInterval = 10 * time.Millisecond
	fixture := createFixture(t, service)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.RunCleanup(ctx)

	// Advance past expiration and give the ticker time to fire.
	clock.Add(24*time.Hour + time.Second)
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, stateExpired, loadState(t, service, fixture.id))
	assert.NoFileExists(t, service.manifestPath(fixture.id))
}

func TestTombstoneOnAlreadyTombstonedTransferIsIdempotent(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createUploadingFixtureOptions(t, service, 0, nil)

	reserved := service.reservedBytes
	require.True(t, reserved > 0)
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	// First expiry clears the reservation and stamps the tombstone.
	require.NoError(t, service.tombstone(meta, stateExpired))
	assert.Zero(t, service.reservedBytes)
	assert.True(t, meta.TombstoneExpiresAt.After(clock.Time()))

	// A second expiry observes the transfer already tombstoned: it must only
	// purge ciphertext again, never double-release quota or touch timestamps.
	meta, err = service.load(fixture.id)
	require.NoError(t, err)
	require.NoError(t, service.tombstone(meta, stateExpired))
	assert.Zero(t, service.reservedBytes)
	assert.Equal(t, stateExpired, meta.State)
	assert.Equal(t, clock.Time().Add(tombstoneLifetime), meta.TombstoneExpiresAt)
}

func TestTombstoneClampsReservedBytesAtZero(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createUploadingFixtureOptions(t, service, 0, nil)

	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	reservation := meta.ReservedBytes
	require.True(t, reservation > 0)

	// A corrupted or restored counter below the reservation must clamp to zero.
	service.mu.Lock()
	service.reservedBytes = reservation / 2
	service.mu.Unlock()

	require.NoError(t, service.tombstone(meta, stateExpired))
	assert.Zero(t, service.reservedBytes)
}

func TestRemoveTransferClampsReservedBytesAtZero(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)

	id, err := storecrypto.GenerateTransferID()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(service.transferDir(id), 0o700))

	service.mu.Lock()
	service.reservedBytes = 10
	service.mu.Unlock()

	require.NoError(t, service.removeTransfer(&metadata{ID: id, ReservedBytes: 100}))
	assert.NoDirExists(t, service.transferDir(id))
	assert.Zero(t, service.reservedBytes)
}

func TestSweepPropagatesCleanupErrors(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	service := newTestService(t, clock)
	fixture := createFixture(t, service)
	require.True(t, service.reservedBytes > 0)

	// A transfer whose directory was made read-only cannot persist the
	// tombstone; the sweep must surface the failure and preserve the quota.
	transferDir := service.transferDir(fixture.id)
	require.NoError(t, os.Chmod(transferDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(transferDir, 0o700) })
	reserved := service.reservedBytes
	require.True(t, reserved > 0)

	clock.Add(24*time.Hour + time.Second)
	err := service.Sweep()
	if err == nil {
		t.Skip("test requires a user without override permission on private directories")
	}
	assert.ErrorContains(t, err, "permission denied")

	// On-disk metadata is unmodified after the failed tombstone; the reserved
	// quota is still held and no partial state leaked.
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateAvailable, meta.State)
	assert.Equal(t, reserved, meta.ReservedBytes)
	assert.Equal(t, reserved, service.reservedBytes)
}
