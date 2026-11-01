package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestInstallerInstallsVerifiedPair(t *testing.T) {
	for _, state := range []string{"success", "Windows", "missing helper", "wrong helper version", "bad checksum", "symlink destination", "install failure"} {
		t.Run(state, func(t *testing.T) {
			work := t.TempDir()
			prefix := filepath.Join(work, "prefix with spaces")
			if err := os.Mkdir(prefix, 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(work, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			suffix := ""
			platform := "Linux"
			extension := "tar.gz"
			if state == "Windows" {
				suffix = ".exe"
				platform = "Windows"
				extension = "zip"
			}
			asset := "croc_v11.6.0_" + platform + "-64bit." + extension
			files := map[string][]byte{}
			for _, tool := range []string{"croc", "croc-update"} {
				if state == "missing helper" && tool == "croc-update" {
					continue
				}
				version := "11.6.0"
				if state == "wrong helper version" && tool == "croc-update" {
					version = "11.5.0"
				}
				files[tool+suffix] = []byte("#!/bin/sh\necho '" + tool + " version " + version + "'\n")
			}
			var archive bytes.Buffer
			if extension == "zip" {
				writer := zip.NewWriter(&archive)
				for name, data := range files {
					entry, _ := writer.Create(name)
					entry.Write(data)
				}
				writer.Close()
			} else {
				gz := gzip.NewWriter(&archive)
				writer := tar.NewWriter(gz)
				for name, data := range files {
					writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
					writer.Write(data)
				}
				writer.Close()
				gz.Close()
			}
			archivePath := filepath.Join(work, asset)
			if err := os.WriteFile(archivePath, archive.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(archive.Bytes())
			if state == "bad checksum" {
				sum[0] ^= 1
			}
			checksums := filepath.Join(work, "checksums.txt")
			if err := os.WriteFile(checksums, fmt.Appendf(nil, "%x  %s\n", sum, asset), 0o644); err != nil {
				t.Fatal(err)
			}
			stubs := map[string]string{
				"uname": "#!/bin/sh\nif [ \"$1\" = -m ]; then echo x86_64; else echo \"$MOCK_OS\"; fi\n",
				"curl":  "#!/bin/bash\ncase \"$*\" in\n *api.github.com*) echo '{\"tag_name\":\"v11.6.0\"}';;\n *) for ((i=1;i<=$#;i++)); do if [[ \"${!i}\" == -o ]]; then j=$((i+1)); dest=\"${!j}\"; fi; done\n case \"$*\" in *_checksums.txt*) cp \"$MOCK_CHECKSUMS\" \"$dest\";; *) cp \"$MOCK_ARCHIVE\" \"$dest\";; esac;;\nesac\n",
				"sudo":  "#!/bin/sh\necho 'unexpected elevation' >&2\nexit 99\n",
				"doas":  "#!/bin/sh\necho 'unexpected elevation' >&2\nexit 99\n",
			}
			if state == "install failure" {
				stubs["install"] = "#!/bin/sh\nexit 1\n"
			}
			for name, data := range stubs {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(data), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			croc := filepath.Join(prefix, "croc"+suffix)
			const original = "original binary"
			if state == "symlink destination" {
				if err := os.Symlink(archivePath, croc); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(croc, []byte(original), 0o755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("/bin/bash", "default.txt", "-p", prefix)
			mockOS := "Linux"
			if state == "Windows" {
				mockOS = "MINGW64_NT"
			}
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "MOCK_OS="+mockOS, "MOCK_CHECKSUMS="+checksums, "MOCK_ARCHIVE="+archivePath)
			output, err := command.CombinedOutput()
			if state == "success" || state == "Windows" {
				if err != nil {
					t.Fatalf("%v\n%s", err, output)
				}
				for name, expected := range files {
					got, err := os.ReadFile(filepath.Join(prefix, name))
					if err != nil || !bytes.Equal(got, expected) {
						t.Fatalf("%s was not installed: %v", name, err)
					}
				}
				if !strings.Contains(string(output), "Installation complete") {
					t.Fatal(string(output))
				}
			} else {
				if err == nil || strings.Contains(string(output), "Installation complete") {
					t.Fatalf("bad installation succeeded: %s", output)
				}
				if state != "symlink destination" {
					got, _ := os.ReadFile(croc)
					if string(got) != original {
						t.Fatal("failed installation replaced croc")
					}
				}
			}
		})
	}
}

func TestInstallerDeterminesLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		response string
	}{
		{
			name: "curl with formatted JSON",
			tool: "curl",
			response: `{
  "url": "https://api.github.com/repos/schollz/croc/releases/1",
  "tag_name": "v11.2.3",
  "draft": false,
  "prerelease": false
}`,
		},
		{
			name:     "wget fallback with compact JSON",
			tool:     "wget",
			response: `{"url":"https://api.github.com/releases/1","tag_name":"v11.2.3","draft":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runInstallerVersionLookup(t, tt.tool, tt.response, 0)
			if err != nil {
				t.Fatalf("lookup with %s: %v", tt.tool, err)
			}
			if got != "11.2.3" {
				t.Fatalf("version = %q, want 11.2.3", got)
			}
		})
	}
}

func TestInstallerLatestVersionFailures(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		response   string
		toolStatus int
		wantStatus int
	}{
		{name: "request failure", tool: "curl", toolStatus: 22, wantStatus: 1},
		{name: "malformed JSON", tool: "curl", response: `{`, wantStatus: 2},
		{name: "missing tag", tool: "curl", response: `{"name":"v11.2.3"}`, wantStatus: 2},
		{name: "unprefixed tag", tool: "curl", response: `{"tag_name":"11.2.3"}`, wantStatus: 2},
		{name: "prerelease tag", tool: "curl", response: `{"tag_name":"v11.2.3-rc.1"}`, wantStatus: 2},
		{name: "leading zero", tool: "wget", response: `{"tag_name":"v11.02.3"}`, wantStatus: 2},
		{name: "no download tool", wantStatus: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runInstallerVersionLookup(t, tt.tool, tt.response, tt.toolStatus)
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("error = %v, want exit status %d", err, tt.wantStatus)
			}
			if got := exitErr.ExitCode(); got != tt.wantStatus {
				t.Fatalf("exit status = %d, want %d", got, tt.wantStatus)
			}
		})
	}
}

func TestInstallerBuildsReleaseAssetURLsFromDynamicVersion(t *testing.T) {
	contents, err := os.ReadFile("default.txt")
	if err != nil {
		t.Fatal(err)
	}
	script := string(contents)
	for _, fragment := range []string{
		`croc_file="${croc_bin_name}_v${croc_version}_${croc_os}-${croc_arch}.${croc_dl_ext}"`,
		`croc_checksum_file="${croc_bin_name}_v${croc_version}_checksums.txt"`,
		`croc_url="${croc_base_url}/v${croc_version}/${croc_file}"`,
		`croc_checksum_url="${croc_base_url}/v${croc_version}/${croc_checksum_file}"`,
		`for name in croc-update croc; do`,
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("installer does not contain URL construction %q", fragment)
		}
	}
	if regexp.MustCompile(`croc_version="[0-9]+\.[0-9]+\.[0-9]+"`).MatchString(script) {
		t.Fatal("installer still contains a hardcoded croc version")
	}
}

func TestLinux32BitTargetsRemainInInstallerAndReleaseBuilds(t *testing.T) {
	contents, err := os.ReadFile("default.txt")
	if err != nil {
		t.Fatal(err)
	}
	script := string(contents)
	for _, fragment := range []string{`armv5l) croc_arch=ARMv5`, `i386|i486|i586|i686) croc_arch=32bit`} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("installer does not map Linux target %q", fragment)
		}
	}
	if strings.Contains(script, "no longer supported") {
		t.Fatal("installer still rejects Linux 32-bit targets")
	}

	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	release, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"GOOS=linux GOARCH=386", "GOOS=linux GOARCH=arm go"} {
		if !strings.Contains(string(ci), required) {
			t.Fatalf("CI workflow omits restored target %q", required)
		}
	}
	for _, required := range []string{"name: Linux-32bit", "name: Linux-ARM\n", "name: Linux-ARMv5"} {
		if !strings.Contains(string(release), required) {
			t.Fatalf("release workflow omits restored artifact %q", required)
		}
	}
}

func runInstallerVersionLookup(t *testing.T, tool, response string, toolStatus int) (string, error) {
	t.Helper()

	contents, err := os.ReadFile("default.txt")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(contents), `main "${INSTALL_PREFIX}"`, "", 1)
	script += "\ndetermine_latest_version\n"

	binDir := t.TempDir()
	if tool != "" {
		toolScript := "#!/bin/sh\nprintf '%s' \"${MOCK_GITHUB_RESPONSE}\"\nexit \"${MOCK_TOOL_STATUS}\"\n"
		toolPath := binDir + string(os.PathSeparator) + tool
		if err := os.WriteFile(toolPath, []byte(toolScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("/bin/bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = []string{
		"PATH=" + binDir,
		"MOCK_GITHUB_RESPONSE=" + response,
		"MOCK_TOOL_STATUS=" + strconv.Itoa(toolStatus),
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}
	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), err
}
