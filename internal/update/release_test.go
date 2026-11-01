package update

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
			got, err := AssetNameForPlatform("11.5.2", test.goos, test.goarch, test.goarm)
			if want := "croc_v11.5.2_" + test.want; err != nil || got != want {
				t.Fatalf("asset = %q, error = %v; want %q", got, err, want)
			}
		})
	}
	for _, version := range []string{"v11.5.2", "11.5.2/other", "11.5.2-rc.1"} {
		t.Run(version, func(t *testing.T) {
			if asset, err := AssetNameForPlatform(version, "linux", "amd64", ""); err == nil || asset != "" {
				t.Fatalf("invalid version accepted: asset=%q, error=%v", asset, err)
			}
		})
	}
}

type interruptedDownload struct{}

func (interruptedDownload) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestDownloadProgress(t *testing.T) {
	for _, state := range []string{"known length", "unknown length", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			payload := []byte(strings.Repeat("archive data", 100))
			var source io.Reader = bytes.NewReader(payload)
			length := int64(len(payload))
			if state == "unknown length" {
				length = -1
			}
			if state == "interrupted" {
				source = io.MultiReader(bytes.NewReader(payload[:100]), interruptedDownload{})
			}
			client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: length, Body: io.NopCloser(source), Header: make(http.Header)}, nil
			})}
			var progress bytes.Buffer
			got, err := downloadUpdate(t.Context(), client, "https://example.test/release.tar.gz", 4096, downloadProgress{Writer: &progress, Description: "Downloading croc"})
			if !strings.Contains(progress.String(), "Downloading croc") {
				t.Fatal("download progress was not rendered")
			}
			if state == "interrupted" {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatal(err)
				}
				if strings.Contains(progress.String(), "100%") {
					t.Fatal("interrupted download reported completion")
				}
				return
			}
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("progress changed download: %v", err)
			}
			if state == "known length" && !strings.Contains(progress.String(), "100%") {
				t.Fatal("download did not show completion")
			}
		})
	}
}
