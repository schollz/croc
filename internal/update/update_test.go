package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func archiveFixture(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	if runtime.GOOS == "windows" {
		writer := zip.NewWriter(&output)
		for name, data := range files {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		compressed := gzip.NewWriter(&output)
		writer := tar.NewWriter(compressed)
		for name, data := range files {
			if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(data))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

func buildFixture(t *testing.T, tool, version string) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), BinaryName(tool))
	cmd := exec.Command("go", "build", "-buildvcs=false", "-ldflags", "-s -w -X main.tool="+tool+" -X main.version="+version, "-o", binary, "./testdata/command")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native fixture: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFixture(t *testing.T, dir, tool string, binary []byte) string {
	t.Helper()
	name := filepath.Join(dir, BinaryName(tool))
	if err := os.WriteFile(name, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	return name
}

// The real commands run as native executables. Their archives also contain
// compiled executables, not scripts (which do not reproduce ETXTBSY/PE locks).
func TestNativeComplementaryUpdates(t *testing.T) {
	oldCroc := buildFixture(t, Croc, "11.5.0")
	oldHelper := buildFixture(t, Helper, "11.5.0")
	newCroc := buildFixture(t, Croc, "11.6.0")
	newHelper := buildFixture(t, Helper, "11.6.0")
	newerHelper := buildFixture(t, Helper, "11.7.0")
	newerCroc := buildFixture(t, Croc, "11.7.0")
	legacyCroc := buildFixture(t, Croc, "v10.4.4")
	asset, err := AssetName("11.6.0")
	if err != nil {
		t.Fatal(err)
	}
	archive := archiveFixture(t, map[string][]byte{BinaryName(Croc): newCroc, BinaryName(Helper): newHelper})
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/latest":
			fmt.Fprint(w, `{"tag_name":"v11.6.0"}`)
		case strings.HasSuffix(r.URL.Path, "_checksums.txt"):
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), asset)
		case strings.HasSuffix(r.URL.Path, asset):
			downloads.Add(1)
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	run := func(t *testing.T, binary string, args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "CROC_UPDATE_TEST_URL="+server.URL, "CROC_CONFIG_DIR="+filepath.Join(t.TempDir(), "absent"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", binary, args, err, output)
		}
		return string(output)
	}
	for _, state := range []string{"missing", "outdated", "current", "newer", "croc current"} {
		t.Run(state, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "custom prefix with spaces")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			dir, err = filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			crocBytes := oldCroc
			if state == "croc current" {
				crocBytes = newCroc
			}
			croc := writeFixture(t, dir, Croc, crocBytes)
			helper := filepath.Join(dir, BinaryName(Helper))
			switch state {
			case "outdated":
				writeFixture(t, dir, Helper, oldHelper)
			case "current":
				writeFixture(t, dir, Helper, newHelper)
			case "newer":
				writeFixture(t, dir, Helper, newerHelper)
			}
			before, _ := os.ReadFile(helper)
			count := downloads.Load()
			run(t, croc, "update", "--check")
			after, _ := os.ReadFile(helper)
			if !bytes.Equal(before, after) || downloads.Load() != count {
				t.Fatal("--check installed an update")
			}
			output := run(t, croc, "update", "--yes")
			if state == "missing" || state == "croc current" {
				installed := strings.Index(output, "Installed croc-update")
				followup := strings.Index(output, "Run: ")
				if installed < 0 || followup <= installed {
					t.Fatalf("follow-up printed before helper installation: %s", output)
				}
			}
			if !strings.Contains(output, ShellCommand(helper, "--path", croc, "--yes")) {
				t.Fatal(output)
			}
			afterCroc, _ := os.ReadFile(croc)
			if !bytes.Equal(afterCroc, crocBytes) {
				t.Fatal("croc update replaced itself")
			}
			want := "11.6.0"
			if state == "newer" {
				want = "11.7.0"
			}
			if got := run(t, helper, "--version"); strings.TrimSpace(got) != "croc-update version "+want {
				t.Fatal(got)
			}
			if (state == "current" || state == "newer") && downloads.Load() != count {
				t.Fatal("downloaded an unnecessary helper update")
			}
			// The pinned command from croc must still select it after PATH changes.
			other := t.TempDir()
			otherCroc := writeFixture(t, other, Croc, oldCroc)
			t.Setenv("PATH", other+string(os.PathListSeparator)+os.Getenv("PATH"))
			helperBefore, _ := os.ReadFile(helper)
			run(t, helper, "--path", croc, "--check")
			downloadOutput := run(t, helper, "--path", croc, "--yes")
			if state != "croc current" && !strings.Contains(downloadOutput, "Downloading croc") {
				t.Fatalf("download progress missing: %s", downloadOutput)
			}
			if state != "croc current" {
				if !strings.HasPrefix(downloadOutput, "Update croc: v11.5.0 → v11.6.0\nReplace: "+croc+"\n") || !strings.Contains(downloadOutput, "Updated croc to v11.6.0.") {
					t.Fatalf("unclear update action: %s", downloadOutput)
				}
				if strings.Count(downloadOutput, croc) != 1 || strings.Contains(downloadOutput, asset) || strings.Contains(downloadOutput, "Release archive") {
					t.Fatalf("duplicate path or archive details in update output: %s", downloadOutput)
				}
			}
			if got := run(t, croc, "--version"); strings.TrimSpace(got) != "croc version 11.6.0" {
				t.Fatal(got)
			}
			helperAfter, _ := os.ReadFile(helper)
			if !bytes.Equal(helperBefore, helperAfter) {
				t.Fatal("croc-update replaced itself")
			}
			otherAfter, _ := os.ReadFile(otherCroc)
			if !bytes.Equal(otherAfter, oldCroc) {
				t.Fatal("updated a different croc from PATH")
			}
			count = downloads.Load()
			run(t, helper, "--path", croc, "--yes")
			if downloads.Load() != count {
				t.Fatal("up-to-date croc was downloaded again")
			}
		})
	}
	t.Run("install missing croc", func(t *testing.T) {
		dir := t.TempDir()
		helper := writeFixture(t, dir, Helper, newHelper)
		// No croc on PATH: the new-install fallback is beside the helper.
		t.Setenv("PATH", t.TempDir())
		run(t, helper, "--yes")
		if got := run(t, filepath.Join(dir, BinaryName(Croc)), "--version"); !strings.Contains(got, "11.6.0") {
			t.Fatal(got)
		}
	})

	t.Run("never downgrade croc", func(t *testing.T) {
		dir := t.TempDir()
		croc := writeFixture(t, dir, Croc, newerCroc)
		helper := writeFixture(t, dir, Helper, oldHelper)
		count := downloads.Load()
		run(t, helper, "--path", croc, "--yes")
		if downloads.Load() != count {
			t.Fatal("downloaded a downgrade")
		}
		if got := run(t, croc, "--version"); strings.TrimSpace(got) != "croc version 11.7.0" {
			t.Fatal(got)
		}
	})
	t.Run("missing helper failure has no follow-up command", func(t *testing.T) {
		dir := t.TempDir()
		croc := writeFixture(t, dir, Croc, oldCroc)
		client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			data := archive
			if strings.HasSuffix(r.URL.Path, "/latest") {
				data = []byte(`{"tag_name":"v11.6.0"}`)
			}
			if strings.HasSuffix(r.URL.Path, "_checksums.txt") {
				data = []byte(strings.Repeat("0", 64) + "  " + asset + "\n")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
		})}
		var output, progress bytes.Buffer
		err := (Options{Tool: Croc, Version: "11.5.0", Executable: croc, Invoked: croc, Yes: true, Writer: &output, ProgressWriter: &progress, Client: client}).Run(t.Context())
		if err == nil || strings.Contains(output.String(), "Run:") {
			t.Fatalf("suggested running a missing helper: %v\n%s", err, output.String())
		}
		if _, err := os.Stat(filepath.Join(dir, BinaryName(Helper))); !os.IsNotExist(err) {
			t.Fatal("failed verification installed a helper")
		}
	})

	t.Run("active PATH Go installation", func(t *testing.T) {
		helperDir := t.TempDir()
		helper := writeFixture(t, helperDir, Helper, newHelper)
		activeDir := filepath.Join(t.TempDir(), "go", "bin")
		if err := os.MkdirAll(activeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		croc := writeFixture(t, activeDir, Croc, legacyCroc)
		// An adjacent croc exists too; PATH must take precedence for direct use.
		adjacent := writeFixture(t, helperDir, Croc, oldCroc)
		t.Setenv("PATH", activeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		output := run(t, helper, "--check")
		activeReal, err := filepath.EvalSymlinks(croc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output, "File: "+activeReal) || !strings.Contains(output, "current: v10.4.4") {
			t.Fatal(output)
		}
		run(t, helper, "--yes")
		if got := run(t, croc, "--version"); strings.TrimSpace(got) != "croc version 11.6.0" {
			t.Fatal(got)
		}
		if got, _ := os.ReadFile(adjacent); !bytes.Equal(got, oldCroc) {
			t.Fatal("default PATH update changed adjacent croc")
		}
		output = run(t, croc, "update", "--yes")
		activeHelper := filepath.Join(filepath.Dir(activeReal), BinaryName(Helper))
		if !strings.Contains(output, ShellCommand(activeHelper, "--path", activeReal, "--yes")) {
			t.Fatal(output)
		}
		if got := run(t, activeHelper, "--version"); strings.TrimSpace(got) != "croc-update version 11.6.0" {
			t.Fatal(got)
		}

	})
	t.Run("failed verification leaves original", func(t *testing.T) {
		dir := t.TempDir()
		croc := writeFixture(t, dir, Croc, oldCroc)
		helper := writeFixture(t, dir, Helper, oldHelper)
		for _, failure := range []string{"checksum", "wrong version", "truncated", "replacement"} {
			t.Run(failure, func(t *testing.T) {
				badArchive := archive
				if failure == "wrong version" {
					badArchive = archiveFixture(t, map[string][]byte{BinaryName(Helper): oldHelper})
				}
				client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
					data := badArchive
					if strings.HasSuffix(r.URL.Path, "_checksums.txt") {
						sum := sha256.Sum256(badArchive)
						if failure == "checksum" {
							sum[0] ^= 1
						}
						data = fmt.Appendf(nil, "%x  %s\n", sum, asset)
					}
					if failure == "replacement" && strings.HasSuffix(r.URL.Path, "_checksums.txt") {
						if err := os.Remove(helper); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(helper, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					if failure == "truncated" {
						return nil, io.ErrUnexpectedEOF
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
				})}
				_, err := install(t.Context(), helper, croc, Helper, "11.6.0", client, releaseDownloadURLFormat, nil)
				if err == nil {
					t.Fatal("invalid update succeeded")
				}
				if failure == "replacement" {
					os.Remove(helper)
					writeFixture(t, dir, Helper, oldHelper)
				}
				got, _ := os.ReadFile(helper)
				if !bytes.Equal(got, oldHelper) {
					t.Fatal("failed update changed original")
				}
			})
		}
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPackageAndSymlinkProtection(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CROC_CONFIG_DIR", config)
	paths := []string{"/usr/bin/croc", "/bin/croc", "/nix/store/hash-croc/bin/croc", "/opt/homebrew/Cellar/croc/11.5.0/bin/croc", "/home/test/scoop/apps/croc/current/croc.exe"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\test\scoop\apps\croc\current\croc.exe`, `C:\ProgramData\chocolatey\bin\croc.exe`, `C:\Users\test\miniconda3\bin\croc.exe`}
	}
	for _, path := range paths {
		if ManagedGuidance(path) == "" {
			t.Errorf("unprotected managed path %s", path)
		}
		if err := os.WriteFile(filepath.Join(config, "install.json"), fmt.Appendf(nil, `{"version":1,"method":"getcroc-installer","target":%q}`, path), 0o600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		err := (Options{Tool: Croc, Version: "11.6.0", Executable: path, Writer: &output, Client: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("managed install contacted release service")
			return nil, nil
		})}}).Run(t.Context())
		if err != nil || output.Len() == 0 {
			t.Fatalf("%v: %s", err, output.String())
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "croc")
	if err := os.Symlink("/usr/bin/croc", target); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStandalone(target); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestDirectoryLockAndCancelledUpdate(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	second, err := lockDirectory(dir)
	if err == nil {
		second()
		t.Fatal("concurrent lock succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = FetchLatest(ctx, NewHTTPClient(0), LatestReleaseEndpoint)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestArchiveRejectsDuplicatesLinksAndUnsafePaths(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip"} {
		for _, kind := range []string{"duplicate", "symlink", "traversal", "missing", "truncated"} {
			t.Run(format+"/"+kind, func(t *testing.T) {
				var output bytes.Buffer
				name := "croc"
				entry := name
				if kind == "traversal" {
					entry = "../croc"
				}
				if kind == "missing" {
					entry = "other"
				}
				count := 1
				if kind == "duplicate" {
					count = 2
				}
				if format == "zip" {
					writer := zip.NewWriter(&output)
					for range count {
						header := &zip.FileHeader{Name: entry}
						header.SetMode(0o755)
						if kind == "symlink" {
							header.SetMode(os.ModeSymlink | 0o755)
						}
						file, _ := writer.CreateHeader(header)
						file.Write([]byte("test"))
					}
					writer.Close()
				} else {
					gz := gzip.NewWriter(&output)
					writer := tar.NewWriter(gz)
					for range count {
						header := &tar.Header{Name: entry, Mode: 0o755, Size: 4, Typeflag: tar.TypeReg}
						if kind == "symlink" {
							header.Typeflag = tar.TypeSymlink
							header.Size = 0
							header.Linkname = "/usr/bin/croc"
						}
						writer.WriteHeader(header)
						if kind != "symlink" {
							writer.Write([]byte("test"))
						}
					}
					writer.Close()
					gz.Close()
				}
				data := output.Bytes()
				if kind == "truncated" {
					data = data[:len(data)/2]
				}
				if err := extractBinary("test."+format, data, name, io.Discard); err == nil {
					t.Fatal("accepted invalid archive")
				}
			})
		}
	}
}
