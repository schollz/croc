package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStandaloneUpdateVerifiesAndReplaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("in-place self-update is intentionally disabled on Windows")
	}
	const version = "11.4.1"
	fixtureAssetName := func(version string) (string, error) {
		return updateAssetNameForPlatform(version, "linux", "amd64", "")
	}
	asset, err := fixtureAssetName(version)
	if err != nil {
		t.Fatal(err)
	}
	archive := updateTestArchive(t, "#!/bin/sh\nprintf 'croc version 11.4.1\\n'\n")
	digest := sha256.Sum256(archive)
	validChecksums := []byte(fmt.Sprintf("%x  %s\n", digest, asset))

	for _, test := range []struct {
		name      string
		checksums []byte
		wantError string
	}{
		{name: "verified replacement", checksums: validChecksums},
		{name: "checksum mismatch", checksums: []byte(strings.Repeat("0", 64) + "  " + asset + "\n"), wantError: "checksum verification failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "croc")
			const original = "#!/bin/sh\nprintf 'croc version 11.4.0\\n'\n"
			if err := os.WriteFile(target, []byte(original), 0o755); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: updateRoundTripper(func(request *http.Request) (*http.Response, error) {
				payload := archive
				if strings.HasSuffix(request.URL.Path, "_checksums.txt") {
					payload = test.checksums
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(payload)),
					Header:     make(http.Header),
				}, nil
			})}
			err := applyStandaloneUpdateWithAssetName(context.Background(), target, version, client, fixtureAssetName)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				contents, readErr := os.ReadFile(target)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(contents) != original {
					t.Fatal("checksum failure changed the installed executable")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(output, []byte("11.4.1")) {
				t.Fatalf("updated executable = %q", output)
			}
		})
	}
}

func TestUpdateAssetNameForPlatform(t *testing.T) {
	for _, test := range []struct {
		goos, goarch, goarm string
		want                string
	}{
		{"linux", "amd64", "", "Linux-64bit.tar.gz"},
		{"linux", "386", "", "Linux-32bit.tar.gz"},
		{"linux", "arm64", "", "Linux-ARM64.tar.gz"},
		{"linux", "arm", "7", "Linux-ARM.tar.gz"},
		{"linux", "arm", "5", "Linux-ARMv5.tar.gz"},
		{"linux", "arm", "5,softfloat", "Linux-ARMv5.tar.gz"},
		{"linux", "riscv64", "", "Linux-RISCV64.tar.gz"},
		{"darwin", "amd64", "", "macOS-64bit.tar.gz"},
		{"darwin", "arm64", "", "macOS-ARM64.tar.gz"},
		{"windows", "amd64", "", "Windows-64bit.zip"},
		{"windows", "386", "", "Windows-32bit.zip"},
		{"windows", "arm64", "", "Windows-ARM64.zip"},
		{"freebsd", "amd64", "", "FreeBSD-64bit.tar.gz"},
		{"freebsd", "arm64", "", "FreeBSD-ARM64.tar.gz"},
		{"netbsd", "amd64", "", "NetBSD-64bit.tar.gz"},
		{"netbsd", "386", "", "NetBSD-32bit.tar.gz"},
		{"netbsd", "arm64", "", "NetBSD-ARM64.tar.gz"},
		{"openbsd", "amd64", "", "OpenBSD-64bit.tar.gz"},
		{"openbsd", "arm64", "", "OpenBSD-ARM64.tar.gz"},
		{"dragonfly", "amd64", "", "DragonFlyBSD-64bit.tar.gz"},
	} {
		t.Run(test.goos+"/"+test.goarch+"/"+test.goarm, func(t *testing.T) {
			got, err := updateAssetNameForPlatform("11.5.2", test.goos, test.goarch, test.goarm)
			if want := "croc_v11.5.2_" + test.want; err != nil || got != want {
				t.Fatalf("asset = %q, error = %v; want %q", got, err, want)
			}
		})
	}
	for _, version := range []string{"v11.5.2", "11.5.2/other", "11.5.2-rc.1"} {
		t.Run(version, func(t *testing.T) {
			if asset, err := updateAssetNameForPlatform(version, "linux", "amd64", ""); err == nil || asset != "" {
				t.Fatalf("invalid version accepted: asset=%q, error=%v", asset, err)
			}
		})
	}
}

func TestStandaloneUpdateRejectsUnsupportedPlatformBeforeDownload(t *testing.T) {
	for _, test := range []struct{ goos, goarch string }{
		{"linux", "ppc64le"}, {"linux", "s390x"}, {"linux", "loong64"},
		{"freebsd", "386"}, {"unknown", "amd64"},
	} {
		t.Run(test.goos+"/"+test.goarch, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "croc")
			const original = "original executable"
			if err := os.WriteFile(target, []byte(original), 0o755); err != nil {
				t.Fatal(err)
			}
			requested := false
			client := &http.Client{Transport: updateRoundTripper(func(*http.Request) (*http.Response, error) {
				requested = true
				return nil, fmt.Errorf("unexpected download")
			})}
			assetName := func(version string) (string, error) {
				return updateAssetNameForPlatform(version, test.goos, test.goarch, "")
			}
			err := applyStandaloneUpdateWithAssetName(t.Context(), target, "11.5.2", client, assetName)
			wantError := "croc releases do not contain an update for " + test.goos + "/" + test.goarch
			if err == nil || err.Error() != wantError || requested {
				t.Fatalf("error=%v, requested=%v; want %q without a download", err, requested, wantError)
			}
			contents, err := os.ReadFile(target)
			if err != nil || string(contents) != original {
				t.Fatalf("unsupported update changed executable: contents=%q, error=%v", contents, err)
			}
			files, err := os.ReadDir(filepath.Dir(target))
			if err != nil || len(files) != 1 {
				t.Fatalf("unsupported update created staging files: files=%v, error=%v", files, err)
			}
		})
	}
}

func TestUpdateGuidanceForUnmanagedInstallations(t *testing.T) {
	t.Setenv("CROC_CONFIG_DIR", t.TempDir())
	target := filepath.Join(t.TempDir(), "croc")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	eligible, reason := registeredWritableTarget(target)
	if eligible || !strings.Contains(reason, "not registered") {
		t.Fatalf("eligible = %v, reason = %q", eligible, reason)
	}

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/opt/homebrew/Cellar/croc/11.4.0/bin/croc", want: "brew upgrade croc"},
		{path: "/nix/store/hash-croc/bin/croc", want: "Nix"},
		{path: "/usr/bin/croc", want: "system package manager"},
	} {
		if got := updateGuidance(test.path); !strings.Contains(got, test.want) {
			t.Errorf("updateGuidance(%q) = %q, want containing %q", test.path, got, test.want)
		}
	}
}

type updateRoundTripper func(*http.Request) (*http.Response, error)

func TestPackageManagedUpdateRejectsStaleRegistration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux package locations")
	}
	t.Setenv("CROC_CONFIG_DIR", t.TempDir())
	for _, target := range []string{"/usr/bin/croc", "/bin/croc", "/nix/store/hash-croc/bin/croc"} {
		path, err := installManifestPath(true)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(installManifest{Version: installManifestVersion, Method: officialInstallerMethod, Target: target})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		if eligible, reason := registeredWritableTarget(target); eligible || !strings.Contains(reason, "package-managed") {
			t.Fatalf("%s: eligible=%v, reason=%q", target, eligible, reason)
		}
		client := &http.Client{Transport: updateRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("package-managed update must not download a replacement")
			return nil, nil
		})}
		if err = applyStandaloneUpdate(context.Background(), target, "11.5.3", client); err == nil || !strings.Contains(err.Error(), "package-managed") {
			t.Fatalf("%s: unexpected update error: %v", target, err)
		}
	}
	if packageManagedLocation("/usr/local/bin/croc") {
		t.Fatal("standalone installer location must remain eligible")
	}
}

func (roundTrip updateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func updateTestArchive(t *testing.T, executable string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "croc", Mode: 0o755, Size: int64(len(executable)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(executable)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
