package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	internalcli "github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/utils"
)

const (
	latestReleaseEndpoint  = "https://api.github.com/repos/schollz/croc/releases/latest"
	versionCheckCacheName  = "version-check.json"
	versionCheckInterval   = 24 * time.Hour
	versionCheckTimeout    = 2 * time.Second
	maxReleaseResponseSize = 1 << 20
	maxVersionCacheSize    = 4 << 10
)

var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type releaseVersion struct {
	major uint64
	minor uint64
	patch uint64
}

func parseReleaseVersion(value string) (releaseVersion, bool) {
	match := releaseVersionPattern.FindStringSubmatch(value)
	if match == nil {
		return releaseVersion{}, false
	}

	parts := make([]uint64, 3)
	for i := range parts {
		parsed, err := strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return releaseVersion{}, false
		}
		parts[i] = parsed
	}
	return releaseVersion{major: parts[0], minor: parts[1], patch: parts[2]}, true
}

func (v releaseVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func newerRelease(latest, current string) bool {
	latestVersion, latestOK := parseReleaseVersion(latest)
	currentVersion, currentOK := parseReleaseVersion(current)
	if !latestOK || !currentOK {
		return false
	}
	if latestVersion.major != currentVersion.major {
		return latestVersion.major > currentVersion.major
	}
	if latestVersion.minor != currentVersion.minor {
		return latestVersion.minor > currentVersion.minor
	}
	return latestVersion.patch > currentVersion.patch
}

type versionCheckCache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
}

type versionCheckResult struct {
	latestVersion string
}

type transferVersionChecker struct {
	cachePath      string
	currentVersion string
	endpoint       string
	client         *http.Client
	now            func() time.Time
	timeout        time.Duration
}

type versionCheckHandle struct {
	cancel context.CancelFunc
	result <-chan versionCheckResult
}

func (h versionCheckHandle) finish() (versionCheckResult, bool) {
	select {
	case result := <-h.result:
		h.cancel()
		return result, true
	default:
		h.cancel()
		return versionCheckResult{}, false
	}
}

func (checker transferVersionChecker) start(parent context.Context, force bool) versionCheckHandle {
	if !force {
		if cached, ok := checker.freshCache(); ok {
			result := make(chan versionCheckResult, 1)
			result <- versionCheckResult{latestVersion: cached.LatestVersion}
			return versionCheckHandle{cancel: func() {}, result: result}
		}
	}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, checker.timeout)
	result := make(chan versionCheckResult, 1)
	go func() {
		latest, err := checker.fetchLatest(ctx)
		cached := versionCheckCache{CheckedAt: checker.now().UTC()}
		if err == nil {
			cached.LatestVersion = latest
		}
		_ = writeVersionCheckCache(checker.cachePath, cached)
		result <- versionCheckResult{latestVersion: cached.LatestVersion}
	}()
	return versionCheckHandle{cancel: cancel, result: result}
}

func (checker transferVersionChecker) freshCache() (versionCheckCache, bool) {
	cached, err := readVersionCheckCache(checker.cachePath)
	if err != nil {
		return versionCheckCache{}, false
	}
	now := checker.now()
	age := now.Sub(cached.CheckedAt)
	if age < 0 || age >= versionCheckInterval {
		return versionCheckCache{}, false
	}
	return cached, true
}

func (checker transferVersionChecker) fetchLatest(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checker.endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "croc/"+checker.currentVersion)

	response, err := checker.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("release service returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxReleaseResponseSize {
		return "", errors.New("release response is too large")
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxReleaseResponseSize+1))
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	version, ok := parseReleaseVersion(payload.TagName)
	if !ok {
		return "", errors.New("release response contains an invalid version")
	}
	return version.String(), nil
}

func readVersionCheckCache(name string) (versionCheckCache, error) {
	file, err := os.Open(name)
	if err != nil {
		return versionCheckCache{}, err
	}
	defer file.Close()

	var cached versionCheckCache
	decoder := json.NewDecoder(io.LimitReader(file, maxVersionCacheSize+1))
	if err := decoder.Decode(&cached); err != nil {
		return versionCheckCache{}, err
	}
	if cached.CheckedAt.IsZero() {
		return versionCheckCache{}, errors.New("version cache is missing its check time")
	}
	if cached.LatestVersion != "" {
		version, ok := parseReleaseVersion(cached.LatestVersion)
		if !ok {
			return versionCheckCache{}, errors.New("version cache contains an invalid version")
		}
		cached.LatestVersion = version.String()
	}
	return cached, nil
}

func writeVersionCheckCache(name string, cached versionCheckCache) error {
	contents, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	return writePrivateConfigFile(name, contents)
}

func defaultTransferVersionChecker() (transferVersionChecker, error) {
	configDir, err := utils.GetConfigDir(true)
	if err != nil {
		return transferVersionChecker{}, err
	}
	return transferVersionChecker{
		cachePath:      filepath.Join(configDir, versionCheckCacheName),
		currentVersion: Version,
		endpoint:       latestReleaseEndpoint,
		client:         &http.Client{Timeout: versionCheckTimeout},
		now:            time.Now,
		timeout:        versionCheckTimeout,
	}, nil
}

func forceTransferVersionCheck() bool {
	return os.Getenv("CROC_DO_CHECK") == "1"
}

func startTransferVersionCheck(c *internalcli.Context) func() {
	checker, err := defaultTransferVersionChecker()
	if err != nil {
		return func() {}
	}
	handle := checker.start(c.Context, forceTransferVersionCheck())
	return func() {
		result, ready := handle.finish()
		if !ready || c.Bool("quiet") || !newerRelease(result.latestVersion, checker.currentVersion) {
			return
		}
		writer := io.Writer(os.Stderr)
		if c.App != nil && c.App.ErrWriter != nil {
			writer = c.App.ErrWriter
		}
		_, _ = fmt.Fprintf(
			writer,
			"A newer croc version is available: v%s (current: v%s).\nRun: curl https://getcroc.com | bash\n",
			result.latestVersion,
			checker.currentVersion,
		)
	}
}
