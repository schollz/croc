package store

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/storecrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createUploadingFixture(t *testing.T, service *Service) storedFixture {
	t.Helper()
	return createUploadingFixtureOptions(t, service, 0, nil)
}

func TestStoredClientIP(t *testing.T) {
	service := &Service{config: Config{TrustedProxies: []netip.Prefix{
		netip.MustParsePrefix("10.0.1.3/32"),
		netip.MustParsePrefix("192.0.2.1/32"),
		netip.MustParsePrefix("2001:db8:1::/64"),
	}}}
	for _, tc := range []struct {
		name, remote, forwarded, want string
	}{
		{"direct", "198.51.100.1:9000", "", "198.51.100.1"},
		{"untrusted spoof", "198.51.100.1:9000", "203.0.113.1", "198.51.100.1"},
		{"proxy", "10.0.1.3:9000", "198.51.100.1", "198.51.100.1"},
		{"mapped peer", "[::ffff:10.0.1.3]:9000", "::ffff:198.51.100.1", "198.51.100.1"},
		{"multiple trusted hops", "10.0.1.3:9000", "198.51.100.1, 192.0.2.1", "198.51.100.1"},
		{"ignore forged leftmost", "10.0.1.3:9000", "203.0.113.1, 198.51.100.1", "198.51.100.1"},
		{"malformed chain", "10.0.1.3:9000", "bad, 198.51.100.1", "10.0.1.3"},
		{"empty chain", "10.0.1.3:9000", "", "10.0.1.3"},
		{"empty hop", "10.0.1.3:9000", "198.51.100.1, ", "10.0.1.3"},
		{"IPv6", "[2001:db8:1::1]:9000", "2001:db8:2::1", "2001:db8:2::1"},
		{"zone rejected", "10.0.1.3:9000", "fe80::1%eth0", "10.0.1.3"},
		{"all trusted", "10.0.1.3:9000", "192.0.2.1", "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			assert.Equal(t, tc.want, service.clientIP(req))
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "10.0.1.3:9000"
	req.Header.Add("X-Forwarded-For", "203.0.113.1")
	req.Header.Add("X-Forwarded-For", "198.51.100.1, 192.0.2.1")
	assert.Equal(t, "198.51.100.1", service.clientIP(req))
}

func TestStoredCreationLimitReasons(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	service := newTestService(t, clock)
	service.config.CreatePerHour = 1
	service.config.MaxActiveUploads = 1
	fixture := createUploadingFixture(t, service)
	limited := request(t, service, http.MethodPost, "/api/v1/store/transfers", "", []byte("{}"))
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "active-uploads", limited.Header().Get("X-Croc-Rate-Limit-Reason"))
	assert.Equal(t, "60", limited.Header().Get("Retry-After"))
	assert.Contains(t, limited.Body.String(), "unfinished")
	revoked := request(t, service, http.MethodDelete, "/api/v1/store/transfers/"+fixture.id, fixture.uploadToken, nil)
	require.Equal(t, http.StatusNoContent, revoked.Code)
	clock.Add(30*time.Minute + 100*time.Millisecond)
	limited = request(t, service, http.MethodPost, "/api/v1/store/transfers", "", []byte("{}"))
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "create-rate", limited.Header().Get("X-Croc-Rate-Limit-Reason"))
	assert.Equal(t, "1800", limited.Header().Get("Retry-After"))
	clock.Add(30 * time.Minute)
	allowed := request(t, service, http.MethodPost, "/api/v1/store/transfers", "", []byte("{}"))
	assert.Equal(t, http.StatusBadRequest, allowed.Code) // admitted, then declaration rejected
	assert.Empty(t, allowed.Header().Get("X-Croc-Rate-Limit-Reason"))
}

func TestStoredSweepSkipsBusyTransfers(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	service := newTestService(t, clock)
	busy := createUploadingFixture(t, service)
	other := createUploadingFixture(t, service)
	// Select another stripe without depending on the randomized lock seed.
	for service.lockFor(busy.id) == service.lockFor(other.id) {
		other = createUploadingFixture(t, service)
	}
	clock.Add(2 * time.Hour)
	lock := service.lockFor(busy.id)
	lock.Lock()
	done := make(chan error, 1)
	go func() { done <- service.Sweep() }()
	select {
	case err := <-done:
		lock.Unlock()
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		lock.Unlock()
		<-done
		t.Fatal("cleanup blocked on a busy transfer")
	}
	meta, err := service.load(other.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	meta, err = service.load(busy.id)
	require.NoError(t, err)
	assert.Equal(t, stateUploading, meta.State)
	require.NoError(t, service.Sweep())
	meta, err = service.load(busy.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	assert.Zero(t, service.reservedBytes)
	for _, n := range service.activeUploads {
		assert.Zero(t, n)
	}
	// Repeated expiration and a revoke of an expired record cannot release twice.
	require.NoError(t, service.Sweep())
	response := request(t, service, http.MethodDelete, "/api/v1/store/transfers/"+busy.id, busy.uploadToken, nil)
	assert.Equal(t, http.StatusGone, response.Code)
	assert.Zero(t, service.reservedBytes)
}

func TestStoredSweepContinuesAfterCorruptMetadata(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	service := newTestService(t, clock)
	fixture := createUploadingFixture(t, service)
	bad := filepath.Join(service.config.Root, "00", "bad", "metadata.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(bad), 0o700))
	require.NoError(t, os.WriteFile(bad, []byte("private-invalid-metadata"), 0o600))
	clock.Add(2 * time.Hour)
	err := service.Sweep()
	require.ErrorContains(t, err, "1 scan errors")
	assert.NotContains(t, err.Error(), "private-invalid-metadata")
	meta, err := service.load(fixture.id)
	require.NoError(t, err)
	assert.Equal(t, stateExpired, meta.State)
	assert.Zero(t, service.reservedBytes)
	_, err = New(service.config)
	require.Error(t, err, "startup must still reject corrupt metadata")
}

func TestStoredUploadReadDeadline(t *testing.T) {
	for _, authorized := range []bool{true, false} {
		t.Run(fmt.Sprintf("authorized=%v", authorized), func(t *testing.T) {
			clock := &testClock{now: time.Unix(1_700_000_000, 0)}
			service := newTestService(t, clock)
			service.config.UploadTimeout = 400 * time.Millisecond
			fixture := createUploadingFixture(t, service)
			server := httptest.NewServer(service)
			defer server.Close()
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(4*time.Second)))
			token := fixture.uploadToken
			if !authorized {
				token = "invalid"
			}
			path := "/api/v1/store/transfers/" + fixture.id + "/chunks/0"
			_, err = fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Length: %d\r\nX-Croc-SHA256: %s\r\n\r\n", path, server.Listener.Addr(), token, len(fixture.chunk), storecrypto.EncodedSHA256(fixture.chunk))
			require.NoError(t, err)
			_, err = conn.Write(fixture.chunk[:1])
			require.NoError(t, err)
			if authorized {
				require.Eventually(t, func() bool {
					matches, _ := filepath.Glob(filepath.Join(service.transferDir(fixture.id), "chunks", ".upload-*"))
					return len(matches) == 1
				}, 300*time.Millisecond, time.Millisecond)
			}
			// A second chunk request must get the lock after the stalled one ends.
			req, err := http.NewRequest(http.MethodPut, server.URL+path, bytes.NewReader(fixture.chunk))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+fixture.uploadToken)
			req.Header.Set("X-Croc-SHA256", storecrypto.EncodedSHA256(fixture.chunk))
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Do(req)
			require.NoError(t, err)
			response.Body.Close()
			require.Equal(t, http.StatusNoContent, response.StatusCode)
			stalled, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err)
			io.Copy(io.Discard, stalled.Body)
			stalled.Body.Close()
			if authorized {
				assert.Equal(t, http.StatusRequestTimeout, stalled.StatusCode)
			} else {
				assert.Equal(t, http.StatusNotFound, stalled.StatusCode)
			}
			matches, err := filepath.Glob(filepath.Join(service.transferDir(fixture.id), "chunks", ".upload-*"))
			require.NoError(t, err)
			assert.Empty(t, matches)
			meta, err := service.load(fixture.id)
			require.NoError(t, err)
			assert.Equal(t, stateUploading, meta.State)
			assert.Positive(t, meta.ReservedBytes)
		})
	}
}

func TestStoredCreateReadDeadline(t *testing.T) {
	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	service := newTestService(t, clock)
	service.config.UploadTimeout = 50 * time.Millisecond
	server := httptest.NewServer(service)
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = fmt.Fprintf(conn, "POST /api/v1/store/transfers HTTP/1.1\r\nHost: %s\r\nContent-Length: 100\r\n\r\n{", server.Listener.Addr())
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	assert.Equal(t, http.StatusRequestTimeout, response.StatusCode)
	service.mu.Lock()
	defer service.mu.Unlock()
	assert.Zero(t, service.reservedBytes)
	assert.Zero(t, service.activeUploads["127.0.0.1"])
}
