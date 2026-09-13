package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	internalcli "github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/utils"
)

const (
	installManifestName      = "install.json"
	installManifestVersion   = 1
	officialInstallerMethod  = "getcroc-installer"
	updateCheckTimeout       = 15 * time.Second
	updateDownloadTimeout    = 2 * time.Minute
	maxUpdateArchiveSize     = 256 << 20
	maxUpdateBinarySize      = 256 << 20
	maxUpdateChecksumsSize   = 1 << 20
	releaseDownloadURLFormat = "https://github.com/schollz/croc/releases/download/v%s/%s"
)

type installManifest struct {
	Version int    `json:"version"`
	Method  string `json:"method"`
	Target  string `json:"target"`
}

func updateCommand(c *internalcli.Context) error {
	if c.Bool("register-installer") {
		return registerInstallerTarget()
	}
	// Package-owned paths must remain protected even with a stale standalone
	// registration, and should not need network access to explain how to update.
	if !c.Bool("check") {
		target, err := runningExecutable()
		if err != nil {
			return fmt.Errorf("locate running croc executable: %w", err)
		}
		if packageManagedLocation(target) {
			writer := io.Writer(os.Stdout)
			if c.App != nil && c.App.Writer != nil {
				writer = c.App.Writer
			}
			_, _ = fmt.Fprintln(writer, updateGuidance(target))
			return nil
		}
	}
	if _, ok := parseReleaseVersion(Version); !ok {
		return fmt.Errorf("cannot update development build %q; install a stable croc release first", Version)
	}
	checker := transferVersionChecker{
		currentVersion: Version,
		endpoint:       latestReleaseEndpoint,
		client:         newUpdateHTTPClient(updateCheckTimeout),
	}
	var err error
	latest, err := checker.fetchLatest(c.Context)
	if err != nil {
		return fmt.Errorf("check for a newer croc release: %w", err)
	}
	writer := io.Writer(os.Stdout)
	if c.App != nil && c.App.Writer != nil {
		writer = c.App.Writer
	}
	if !newerRelease(latest, Version) {
		_, _ = fmt.Fprintf(writer, "croc v%s is up to date.\n", Version)
		return nil
	}
	_, _ = fmt.Fprintf(writer, "croc v%s is available (current: v%s).\n", latest, Version)
	if c.Bool("check") {
		return nil
	}

	target, err := runningExecutable()
	if err != nil {
		return fmt.Errorf("locate running croc executable: %w", err)
	}
	if executableInvocationUsesSymlink(target) {
		_, _ = fmt.Fprintf(writer, "The running croc command resolves through a symlink and will not be overwritten.\n%s\n", updateGuidance(target))
		return nil
	}
	eligible, reason := registeredWritableTarget(target)
	if !eligible {
		_, _ = fmt.Fprintf(writer, "%s\n%s\n", reason, updateGuidance(target))
		return nil
	}
	if !c.Bool("yes") {
		choice, promptErr := utils.GetInputContext(c.Context, fmt.Sprintf("Update croc from v%s to v%s? (y/N) ", Version, latest))
		if promptErr != nil {
			if ctxErr := c.Context.Err(); ctxErr != nil {
				return ctxErr
			}
			return errors.New("update confirmation requires an interactive terminal; rerun with --yes")
		}
		if !strings.EqualFold(choice, "y") && !strings.EqualFold(choice, "yes") {
			_, _ = fmt.Fprintln(writer, "Update cancelled.")
			return nil
		}
	}
	if err = applyStandaloneUpdate(c.Context, target, latest, newUpdateHTTPClient(updateDownloadTimeout)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "Updated croc from v%s to v%s.\n", Version, latest)
	return nil
}

func newUpdateHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("refusing a non-HTTPS update redirect")
			}
			return nil
		},
	}
}

func runningExecutable() (string, error) {
	target, err := os.Executable()
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	return filepath.Clean(target), nil
}

func executableInvocationUsesSymlink(target string) bool {
	invoked := os.Args[0]
	var err error
	if !strings.ContainsRune(invoked, os.PathSeparator) {
		invoked, err = exec.LookPath(invoked)
		if err != nil {
			return false
		}
	}
	invoked, err = filepath.Abs(invoked)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(invoked)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(target) {
		return false
	}
	return filepath.Clean(invoked) != filepath.Clean(resolved)
}

func installManifestPath(create bool) (string, error) {
	directory, err := utils.GetConfigDir(create)
	if err != nil {
		return "", err
	}
	if directory == "" {
		return "", errors.New("croc configuration directory is unavailable")
	}
	return filepath.Join(directory, installManifestName), nil
}

func registerInstallerTarget() error {
	target, err := runningExecutable()
	if err != nil {
		return err
	}
	if packageManagedLocation(target) {
		return errors.New("cannot register a package-managed executable for standalone updates")
	}
	path, err := installManifestPath(true)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(installManifest{
		Version: installManifestVersion,
		Method:  officialInstallerMethod,
		Target:  target,
	})
	if err != nil {
		return err
	}
	return writePrivateConfigFile(path, payload)
}

func registeredWritableTarget(target string) (bool, string) {
	if packageManagedLocation(target) {
		return false, "This executable is installed in a package-managed location."
	}
	if runtime.GOOS == "windows" {
		return false, "This platform cannot safely replace the running executable in place."
	}
	manifestPath, err := installManifestPath(false)
	if err != nil {
		return false, "This installation is not registered as an official standalone install."
	}
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		return false, "This installation is not registered as an official standalone install."
	}
	var manifest installManifest
	if json.Unmarshal(payload, &manifest) != nil ||
		manifest.Version != installManifestVersion ||
		manifest.Method != officialInstallerMethod ||
		filepath.Clean(manifest.Target) != filepath.Clean(target) {
		return false, "The official installer registration does not match the running executable."
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, "The running executable is not a replaceable regular file."
	}
	writable, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		return false, "The running executable is not writable without privilege escalation or may be immutable."
	}
	_ = writable.Close()
	probe, err := os.CreateTemp(filepath.Dir(target), ".croc-update-probe-*")
	if err != nil {
		return false, "The executable directory is not writable without privilege escalation."
	}
	probeName := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probeName)
	return true, ""
}

func packageManagedLocation(target string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	target = filepath.Clean(target)
	return target == "/usr/bin/croc" || target == "/bin/croc" || strings.HasPrefix(target, "/nix/store/")
}

func updateGuidance(target string) string {
	normalized := filepath.ToSlash(strings.ToLower(target))
	switch {
	case strings.Contains(normalized, "/cellar/croc/") || strings.Contains(normalized, "/linuxbrew/"):
		return "Upgrade with Homebrew: brew upgrade croc"
	case strings.HasPrefix(normalized, "/nix/store/"):
		return "Upgrade croc through the Nix profile or NixOS configuration that owns it."
	case strings.Contains(normalized, "/scoop/apps/croc/"):
		return "Upgrade with Scoop: scoop update croc"
	case strings.Contains(normalized, "/chocolatey/"):
		return "Upgrade with Chocolatey: choco upgrade croc"
	case strings.Contains(normalized, "/conda/") || strings.Contains(normalized, "/miniconda") || strings.Contains(normalized, "/anaconda"):
		return "Upgrade with Conda: conda update --channel conda-forge croc"
	case normalized == "/opt/local/bin/croc":
		return "Upgrade with MacPorts: sudo port selfupdate && sudo port upgrade croc"
	case strings.Contains(normalized, "/com.termux/files/usr/bin/croc"):
		return "Upgrade with Termux pkg: pkg upgrade croc"
	case runtime.GOOS == "freebsd" && normalized == "/usr/local/bin/croc":
		return "Upgrade with FreeBSD pkg: pkg upgrade croc (or rerun the official installer if it owns this file)"
	case normalized == "/usr/bin/croc" || normalized == "/bin/croc":
		return "Upgrade croc with the system package manager that installed it (for example apt, dnf, zypper, pacman, or apk)."
	case strings.HasSuffix(normalized, "/go/bin/croc"):
		return "Upgrade the Go installation: go install github.com/schollz/croc/v11@latest"
	case runtime.GOOS == "windows":
		return "Download the latest release from https://github.com/schollz/croc/releases/latest"
	default:
		prefix := shellQuote(filepath.Dir(target))
		return "Upgrade with the official installer: curl https://getcroc.com | bash -s -- -p " + prefix
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func applyStandaloneUpdate(ctx context.Context, target, version string, client *http.Client) error {
	return applyStandaloneUpdateWithAssetName(ctx, target, version, client, updateAssetName)
}

func applyStandaloneUpdateWithAssetName(ctx context.Context, target, version string, client *http.Client, assetName func(string) (string, error)) error {
	if packageManagedLocation(target) {
		return errors.New("refusing to replace a package-managed executable")
	}
	asset, err := assetName(version)
	if err != nil {
		return err
	}
	archiveURL := fmt.Sprintf(releaseDownloadURLFormat, version, asset)
	checksumsName := fmt.Sprintf("croc_v%s_checksums.txt", version)
	checksumsURL := fmt.Sprintf(releaseDownloadURLFormat, version, checksumsName)
	archive, err := downloadUpdate(ctx, client, archiveURL, maxUpdateArchiveSize)
	if err != nil {
		return fmt.Errorf("download croc update: %w", err)
	}
	checksums, err := downloadUpdate(ctx, client, checksumsURL, maxUpdateChecksumsSize)
	if err != nil {
		return fmt.Errorf("download croc checksums: %w", err)
	}
	if err = verifyUpdateChecksum(asset, archive, checksums); err != nil {
		return err
	}
	current, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	if !current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace a non-regular or symlinked executable")
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".croc-update-*")
	if err != nil {
		return fmt.Errorf("stage croc update: %w", err)
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)
	if err = extractUpdateBinary(asset, archive, staged); err == nil {
		err = staged.Chmod(current.Mode().Perm())
	}
	if err == nil {
		err = staged.Sync()
	}
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("stage croc update: %w", err)
	}
	verification, err := exec.CommandContext(ctx, stagedName, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify staged croc executable: %w", err)
	}
	if got, want := strings.TrimSpace(string(verification)), "croc version "+version; got != want {
		return fmt.Errorf("staged croc reports %q; expected %q", got, want)
	}
	if err = os.Rename(stagedName, target); err != nil {
		return fmt.Errorf("replace croc executable: %w", err)
	}
	return nil
}

func downloadUpdate(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "croc/"+Version)
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
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("release file is too large")
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

func extractUpdateBinary(asset string, archive []byte, destination io.Writer) error {
	if strings.HasSuffix(asset, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return err
		}
		for _, file := range reader.File {
			if file.Name != "croc.exe" {
				continue
			}
			if file.UncompressedSize64 > maxUpdateBinarySize {
				return errors.New("update executable is too large")
			}
			source, err := file.Open()
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(destination, io.LimitReader(source, maxUpdateBinarySize+1))
			closeErr := source.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
			if written != int64(file.UncompressedSize64) || written > maxUpdateBinarySize {
				return errors.New("update archive contains a truncated or oversized croc executable")
			}
			return nil
		}
		return errors.New("update archive does not contain croc.exe")
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Name != "croc" {
			continue
		}
		if !header.FileInfo().Mode().IsRegular() || header.Size < 0 || header.Size > maxUpdateBinarySize {
			return errors.New("update archive contains an invalid croc executable")
		}
		written, copyErr := io.Copy(destination, io.LimitReader(tarReader, maxUpdateBinarySize+1))
		if copyErr != nil {
			return copyErr
		}
		if written != header.Size {
			return errors.New("update archive contains a truncated croc executable")
		}
		return nil
	}
	return errors.New("update archive does not contain croc")
}

func updateAssetName(version string) (string, error) {
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
	return updateAssetNameForPlatform(version, runtime.GOOS, runtime.GOARCH, goarm)
}

func updateAssetNameForPlatform(version, goos, goarch, goarm string) (string, error) {
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
