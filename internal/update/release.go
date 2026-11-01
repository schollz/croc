// Package update installs the other executable in a standalone croc installation.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

const (
	LatestReleaseEndpoint    = "https://api.github.com/repos/schollz/croc/releases/latest"
	releaseDownloadURLFormat = "https://github.com/schollz/croc/releases/download/v%s/%s"
	maxUpdateArchiveSize     = 256 << 20
	maxUpdateBinarySize      = 256 << 20
	maxUpdateChecksumsSize   = 1 << 20
)

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" {
			return errors.New("refusing a non-HTTPS update redirect")
		}
		if len(via) >= 10 {
			return errors.New("too many update redirects")
		}
		return nil
	}}
}

func FetchLatest(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	data, err := downloadUpdate(ctx, client, endpoint, 1<<20)
	if err != nil {
		return "", err
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", err
	}
	v, ok := parseReleaseVersion(release.TagName)
	if !ok || release.Draft || release.Prerelease {
		return "", errors.New("release response does not identify a stable release")
	}
	return v.String(), nil
}

func ValidVersion(v string) bool {
	parsed, ok := parseReleaseVersion(v)
	return ok && parsed.String() == v
}
func Newer(latest, current string) bool { return newerRelease(latest, current) }

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

func downloadUpdate(ctx context.Context, client *http.Client, url string, limit int64, progress ...io.Writer) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "croc-updater")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("release service returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("release file is too large")
	}
	reader := io.Reader(io.LimitReader(response.Body, limit+1))
	var bar *progressbar.ProgressBar
	if len(progress) > 0 && progress[0] != nil {
		total := response.ContentLength
		if total <= 0 {
			total = -1
		}
		fmt.Fprintln(progress[0], "Downloading "+path.Base(request.URL.Path))
		bar = progressbar.NewOptions64(total,
			progressbar.OptionSetWriter(progress[0]),
			progressbar.OptionSetDescription("Release archive"),
			progressbar.OptionSetWidth(20),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionSetRenderBlankState(true),
			// Render on received data, including unknown-length responses, so
			// cancellation never leaves an asynchronous spinner behind.
			progressbar.OptionSetSpinnerChangeInterval(0),
		)
		defer func() { _ = bar.Exit(); fmt.Fprintln(progress[0]) }()
		reader = io.TeeReader(reader, bar)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("release file is too large")
	}
	if bar != nil {
		_ = bar.Finish()
	}
	return payload, nil
}

func verifyUpdateChecksum(asset string, archive, checksums []byte) error {
	want := ""
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			if want != "" {
				return errors.New("release checksums contain duplicate entries for the update asset")
			}
			want = fields[0]
		}
	}
	decoded, err := hex.DecodeString(want)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("release checksums do not contain a valid entry for the update asset")
	}
	got := sha256.Sum256(archive)
	if !bytes.Equal(got[:], decoded) {
		return errors.New("croc update checksum verification failed")
	}
	return nil
}

func AssetName(version string) (string, error) {
	var goarm string
	if runtime.GOARCH == "arm" {
		if buildInfo, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range buildInfo.Settings {
				if setting.Key == "GOARM" {
					goarm = setting.Value
				}
			}
		}
	}
	return AssetNameForPlatform(version, runtime.GOOS, runtime.GOARCH, goarm)
}

func AssetNameForPlatform(version, goos, goarch, goarm string) (string, error) {
	if parsed, ok := parseReleaseVersion(version); !ok || parsed.String() != version {
		return "", fmt.Errorf("invalid croc update version %q", version)
	}
	supported := map[string]map[string]bool{
		"windows":   {"amd64": true, "386": true, "arm64": true},
		"linux":     {"amd64": true, "386": true, "arm": true, "arm64": true, "riscv64": true},
		"darwin":    {"amd64": true, "arm64": true},
		"dragonfly": {"amd64": true},
		"freebsd":   {"amd64": true, "arm64": true},
		"netbsd":    {"386": true, "amd64": true, "arm64": true},
		"openbsd":   {"amd64": true, "arm64": true},
	}
	if !supported[goos][goarch] {
		return "", fmt.Errorf("croc releases do not contain an update for %s/%s", goos, goarch)
	}
	arch := map[string]string{
		"amd64":   "64bit",
		"386":     "32bit",
		"arm64":   "ARM64",
		"riscv64": "RISCV64",
	}[goarch]
	if goarch == "arm" {
		arch = "ARM"
		if strings.HasPrefix(goarm, "5") {
			arch = "ARMv5"
		}
	}
	if arch == "" {
		return "", fmt.Errorf("croc releases do not contain an update for %s/%s", goos, goarch)
	}
	osName := map[string]string{
		"darwin":    "macOS",
		"linux":     "Linux",
		"windows":   "Windows",
		"freebsd":   "FreeBSD",
		"openbsd":   "OpenBSD",
		"netbsd":    "NetBSD",
		"dragonfly": "DragonFlyBSD",
	}[goos]
	if osName == "" {
		return "", fmt.Errorf("croc releases do not contain an update for %s/%s", goos, goarch)
	}
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("croc_v%s_%s-%s%s", version, osName, arch, extension), nil
}
