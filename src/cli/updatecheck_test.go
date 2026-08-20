package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	internalcli "github.com/schollz/croc/v11/internal/cli"
)

func TestParseReleaseVersion(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "11.2.3", want: "11.2.3", ok: true},
		{value: "v11.2.3", want: "11.2.3", ok: true},
		{value: "0.0.0", want: "0.0.0", ok: true},
		{value: "11.2", ok: false},
		{value: "11.02.3", ok: false},
		{value: "v11.2.3-rc.1", ok: false},
		{value: " 11.2.3", ok: false},
		{value: "11.2.3 ", ok: false},
		{value: "18446744073709551616.0.0", ok: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, ok := parseReleaseVersion(test.value)
			if ok != test.ok {
				t.Fatalf("parseReleaseVersion(%q) ok = %v, want %v", test.value, ok, test.ok)
			}
			if ok && got.String() != test.want {
				t.Fatalf("parseReleaseVersion(%q) = %q, want %q", test.value, got.String(), test.want)
			}
		})
	}
}

func TestNewerRelease(t *testing.T) {
	for _, test := range []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{name: "major", latest: "12.0.0", current: "11.9.9", want: true},
		{name: "minor", latest: "11.3.0", current: "11.2.9", want: true},
		{name: "patch", latest: "v11.2.4", current: "11.2.3", want: true},
		{name: "equal", latest: "11.2.3", current: "11.2.3"},
		{name: "older", latest: "10.9.9", current: "11.0.0"},
		{name: "invalid latest", latest: "latest", current: "11.2.3"},
		{name: "invalid current", latest: "11.2.3", current: "development"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := newerRelease(test.latest, test.current); got != test.want {
				t.Fatalf("newerRelease(%q, %q) = %v, want %v", test.latest, test.current, got, test.want)
			}
		})
	}
}

func TestFetchLatestRelease(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		want       string
		wantErr    bool
	}{
		{name: "valid", statusCode: http.StatusOK, body: `{"tag_name":"v11.2.3"}`, want: "11.2.3"},
		{name: "invalid json", statusCode: http.StatusOK, body: `{`, wantErr: true},
		{name: "invalid tag", statusCode: http.StatusOK, body: `{"tag_name":"v11.2.3-rc.1"}`, wantErr: true},
		{name: "missing tag", statusCode: http.StatusOK, body: `{}`, wantErr: true},
		{name: "http failure", statusCode: http.StatusTooManyRequests, body: `{}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Errorf("Accept header = %q", got)
				}
				if got := request.Header.Get("User-Agent"); got != "croc/11.2.2" {
					t.Errorf("User-Agent header = %q", got)
				}
				response.WriteHeader(test.statusCode)
				_, _ = io.WriteString(response, test.body)
			}))
			defer server.Close()

			checker := transferVersionChecker{
				currentVersion: "11.2.2",
				endpoint:       server.URL,
				client:         server.Client(),
			}
			got, err := checker.fetchLatest(context.Background())
			if test.wantErr {
				if err == nil {
					t.Fatalf("fetchLatest() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("fetchLatest() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFetchLatestReleaseHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		close(stopped)
		return nil, request.Context().Err()
	})}
	checker := transferVersionChecker{
		currentVersion: "11.2.2",
		endpoint:       "https://example.test/latest",
		client:         client,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := checker.fetchLatest(ctx)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("fetchLatest succeeded after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("fetchLatest did not stop after cancellation")
	}
	<-stopped
}

func TestVersionCheckUsesFreshCache(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(t.TempDir(), versionCheckCacheName)
	if err := writeVersionCheckCache(cachePath, versionCheckCache{
		CheckedAt:     now.Add(-time.Hour),
		LatestVersion: "11.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	checker, closeServer := newTestVersionChecker(t, cachePath, now, &requests, http.StatusOK, `{"tag_name":"v99.0.0"}`)
	defer closeServer()
	handle := checker.start(context.Background(), false)
	result, ready := handle.finish()
	if !ready || result.latestVersion != "11.2.3" {
		t.Fatalf("fresh result = (%q, %v), want (11.2.3, true)", result.latestVersion, ready)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("fresh cache made %d requests, want 0", got)
	}
}

func TestVersionCheckRefreshesStaleAndCorruptCaches(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		contents []byte
	}{
		{name: "missing"},
		{name: "stale", contents: mustMarshalCache(t, versionCheckCache{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "11.2.2"})},
		{name: "future", contents: mustMarshalCache(t, versionCheckCache{CheckedAt: now.Add(time.Hour), LatestVersion: "11.2.2"})},
		{name: "corrupt", contents: []byte(`not-json`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cachePath := filepath.Join(t.TempDir(), versionCheckCacheName)
			if test.contents != nil {
				if err := os.WriteFile(cachePath, test.contents, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int32
			checker, closeServer := newTestVersionChecker(t, cachePath, now, &requests, http.StatusOK, `{"tag_name":"v11.2.3"}`)
			defer closeServer()
			result := awaitVersionCheck(t, checker.start(context.Background(), false))
			if result.latestVersion != "11.2.3" {
				t.Fatalf("latest version = %q", result.latestVersion)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("requests = %d, want 1", got)
			}
			cached, err := readVersionCheckCache(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			if cached.CheckedAt != now || cached.LatestVersion != "11.2.3" {
				t.Fatalf("updated cache = %#v", cached)
			}
		})
	}
}

func TestForcedVersionCheckBypassesFreshCache(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(t.TempDir(), versionCheckCacheName)
	if err := writeVersionCheckCache(cachePath, versionCheckCache{
		CheckedAt:     now,
		LatestVersion: "11.2.2",
	}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	checker, closeServer := newTestVersionChecker(t, cachePath, now, &requests, http.StatusOK, `{"tag_name":"v11.2.3"}`)
	defer closeServer()
	result := awaitVersionCheck(t, checker.start(context.Background(), true))
	if result.latestVersion != "11.2.3" || requests.Load() != 1 {
		t.Fatalf("forced result = %q with %d requests", result.latestVersion, requests.Load())
	}
}

func TestFailedVersionCheckIsCachedSilently(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(t.TempDir(), versionCheckCacheName)
	var requests atomic.Int32
	checker, closeServer := newTestVersionChecker(t, cachePath, now, &requests, http.StatusServiceUnavailable, `{}`)
	defer closeServer()
	result := awaitVersionCheck(t, checker.start(context.Background(), true))
	if result.latestVersion != "" {
		t.Fatalf("failed check returned version %q", result.latestVersion)
	}
	cached, err := readVersionCheckCache(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if cached.CheckedAt != now || cached.LatestVersion != "" {
		t.Fatalf("failed check cache = %#v", cached)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("cache permissions = %o, want 600", got)
		}
	}
}

func TestVersionCheckCacheWriteFailureDoesNotHideResult(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	cachePath := t.TempDir()
	var requests atomic.Int32
	checker, closeServer := newTestVersionChecker(t, cachePath, now, &requests, http.StatusOK, `{"tag_name":"v11.2.3"}`)
	defer closeServer()
	result := awaitVersionCheck(t, checker.start(context.Background(), true))
	if result.latestVersion != "11.2.3" {
		t.Fatalf("result after cache write failure = %q", result.latestVersion)
	}
}

func TestFinishingVersionCheckNeverWaitsForNetwork(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		close(stopped)
		return nil, request.Context().Err()
	})}
	checker := transferVersionChecker{
		cachePath:      filepath.Join(t.TempDir(), versionCheckCacheName),
		currentVersion: "11.2.2",
		endpoint:       "https://example.test/latest",
		client:         client,
		now:            time.Now,
		timeout:        time.Minute,
	}
	handle := checker.start(context.Background(), true)
	<-started
	start := time.Now()
	if _, ready := handle.finish(); ready {
		t.Fatal("unfinished check unexpectedly had a result")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("finish blocked for %s", elapsed)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("unfinished request was not canceled")
	}
}

func TestCROCDoCheckRequiresExactValue(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: ""},
		{value: "true"},
		{value: "01"},
		{value: "1", want: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("CROC_DO_CHECK", test.value)
			if got := forceTransferVersionCheck(); got != test.want {
				t.Fatalf("forceTransferVersionCheck() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTransferVersionNoticeIsDeferredAndHonorsQuiet(t *testing.T) {
	current, ok := parseReleaseVersion(Version)
	if !ok {
		t.Fatalf("test requires a valid current version, got %q", Version)
	}
	latest := fmt.Sprintf("%d.0.0", current.major+1)
	want := fmt.Sprintf(
		"A newer croc version is available: v%s (current: v%s).\nRun: curl https://getcroc.com | bash\n",
		latest,
		Version,
	)

	for _, test := range []struct {
		name  string
		quiet bool
		want  string
	}{
		{name: "notice", want: want},
		{name: "quiet", quiet: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("CROC_CONFIG_DIR", configDir)
			t.Setenv("CROC_DO_CHECK", "0")
			if err := writeVersionCheckCache(filepath.Join(configDir, versionCheckCacheName), versionCheckCache{
				CheckedAt:     time.Now(),
				LatestVersion: latest,
			}); err != nil {
				t.Fatal(err)
			}

			set := flag.NewFlagSet("test", flag.ContinueOnError)
			set.Bool("quiet", test.quiet, "")
			app := internalcli.NewApp()
			var output bytes.Buffer
			app.ErrWriter = &output
			ctx := internalcli.NewContext(app, set, nil)
			finish := startTransferVersionCheck(ctx)
			if output.Len() != 0 {
				t.Fatalf("notice was written before finish: %q", output.String())
			}
			finish()
			if got := output.String(); got != test.want {
				t.Fatalf("notice = %q, want %q", got, test.want)
			}
		})
	}
}

func TestForcedTransferVersionCheckReportsBeforeTransfer(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		quiet      bool
		want       string
	}{
		{
			name:       "current",
			statusCode: http.StatusOK,
			body:       `{"tag_name":"v11.2.3"}`,
			want:       "No newer croc version is available (current: v11.2.3, latest: v11.2.3).\n",
		},
		{
			name:       "newer",
			statusCode: http.StatusOK,
			body:       `{"tag_name":"v11.2.4"}`,
			want:       "A newer croc version is available: v11.2.4 (current: v11.2.3).\nRun: curl https://getcroc.com | bash\n",
		},
		{
			name:       "unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       `{}`,
			want:       "Could not check for a newer croc version (current: v11.2.3).\n",
		},
		{
			name:       "quiet",
			statusCode: http.StatusOK,
			body:       `{"tag_name":"v11.2.3"}`,
			quiet:      true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			checker, closeServer := newTestVersionChecker(
				t,
				filepath.Join(t.TempDir(), versionCheckCacheName),
				time.Now(),
				&requests,
				test.statusCode,
				test.body,
			)
			defer closeServer()
			checker.currentVersion = "11.2.3"

			set := flag.NewFlagSet("test", flag.ContinueOnError)
			set.Bool("quiet", test.quiet, "")
			app := internalcli.NewApp()
			var output bytes.Buffer
			app.ErrWriter = &output
			ctx := internalcli.NewContext(app, set, nil)

			finish := startTransferVersionCheckWithChecker(ctx, checker, true)
			if got := output.String(); got != test.want {
				t.Fatalf("notice before transfer = %q, want %q", got, test.want)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("requests = %d, want 1", got)
			}
			finish()
			if got := output.String(); got != test.want {
				t.Fatalf("notice after finish = %q, want no duplicate", got)
			}
		})
	}
}

func TestFailedSendAndReceiveRunDeferredVersionNotice(t *testing.T) {
	current, ok := parseReleaseVersion(Version)
	if !ok {
		t.Fatalf("test requires a valid current version, got %q", Version)
	}
	latest := fmt.Sprintf("%d.0.0", current.major+1)
	for _, test := range []struct {
		name        string
		args        []string
		storedToken string
	}{
		{name: "send", args: []string{"croc", "--ignore-stdin", "send"}},
		{name: "stored send", args: []string{"croc", "--ignore-stdin", "send", "--store"}},
		{name: "stored receive", args: []string{"croc", "--ignore-stdin"}, storedToken: "invalid-stored-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("CROC_CONFIG_DIR", configDir)
			t.Setenv("CROC_DO_CHECK", "0")
			t.Setenv("CROC_STORE_TOKEN", test.storedToken)
			if err := writeVersionCheckCache(filepath.Join(configDir, versionCheckCacheName), versionCheckCache{
				CheckedAt:     time.Now(),
				LatestVersion: latest,
			}); err != nil {
				t.Fatal(err)
			}
			app := newApp()
			app.Writer = io.Discard
			var errors bytes.Buffer
			app.ErrWriter = &errors
			if err := app.Run(test.args); err == nil {
				t.Fatalf("%v unexpectedly succeeded", test.args)
			}
			if errors.Len() == 0 {
				t.Fatalf("%v did not run the deferred notice", test.args)
			}
		})
	}
}

func newTestVersionChecker(
	t *testing.T,
	cachePath string,
	now time.Time,
	requests *atomic.Int32,
	statusCode int,
	body string,
) (transferVersionChecker, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response.WriteHeader(statusCode)
		_, _ = io.WriteString(response, body)
	}))
	return transferVersionChecker{
		cachePath:      cachePath,
		currentVersion: "11.2.2",
		endpoint:       server.URL,
		client:         server.Client(),
		now:            func() time.Time { return now },
		timeout:        time.Second,
	}, server.Close
}

func awaitVersionCheck(t *testing.T, handle versionCheckHandle) versionCheckResult {
	t.Helper()
	select {
	case result := <-handle.result:
		handle.cancel()
		return result
	case <-time.After(2 * time.Second):
		handle.cancel()
		t.Fatal("version check did not finish")
		return versionCheckResult{}
	}
}

func mustMarshalCache(t *testing.T, cached versionCheckCache) []byte {
	t.Helper()
	contents, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
