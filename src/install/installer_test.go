package main

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

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
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("installer does not contain URL construction %q", fragment)
		}
	}
	if regexp.MustCompile(`croc_version="[0-9]+\.[0-9]+\.[0-9]+"`).MatchString(script) {
		t.Fatal("installer still contains a hardcoded croc version")
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
