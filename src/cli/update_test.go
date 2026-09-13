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
	asset, err := updateAssetName(version)
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
			err := applyStandaloneUpdate(context.Background(), target, version, client)
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
