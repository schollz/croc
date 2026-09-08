package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	storeapi "github.com/schollz/croc/v11/src/store"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
	"github.com/schollz/croc/v11/src/storeclient"
	"github.com/schollz/croc/v11/src/termui"
)

func TestStoredCallbacksClearPreviousLine(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, false, "Uploading")
	progress := storeclient.Progress{
		FileName:   "LICENSE",
		FileCount:  1,
		TotalBytes: 100,
		TotalSize:  100,
	}
	callbacks.Progress(progress)
	callbacks.Status("Encrypted upload complete")

	got := output.String()
	if !strings.Contains(got, "Uploading LICENSE") || !strings.Contains(got, "100% |") {
		t.Fatalf("stored progress output has no completed bar: %q", got)
	}
	if !strings.Contains(got, "\n\rEncrypted upload complete") {
		t.Fatalf("stored completion status did not follow the bar: %q", got)
	}
}

func TestStoredCallbacksClearUnicodeLineByDisplayWidth(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, false, "Uploading")
	callbacks.Status("🎉")
	callbacks.Status("Done")

	want := "\r🎉\r  \rDone"
	if got := output.String(); got != want {
		t.Fatalf("stored Unicode status output = %q; want %q", got, want)
	}
}

func TestStoredCallbacksQuiet(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, true, "Uploading")
	callbacks.Status("Preparing")
	callbacks.Progress(storeclient.Progress{
		FileName:   "LICENSE",
		TotalBytes: 50,
		TotalSize:  100,
	})
	if output.Len() != 0 {
		t.Fatalf("quiet stored progress output = %q; want no output", output.String())
	}
}

func TestStoredCallbacksUseRegularCrocPalette(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStyledStoredCallbacks(&output, false, true, "Uploading")
	callbacks.Progress(storeclient.Progress{
		FileName:   "LICENSE",
		FileCount:  1,
		TotalBytes: 50,
		TotalSize:  100,
	})
	got := output.String()
	if !strings.Contains(got, termui.Bold+"LICENSE"+termui.Reset) {
		t.Fatalf("stored progress filename is not bold: %q", got)
	}
	callbacks.Progress(storeclient.Progress{FileName: "LICENSE", FileCount: 1, TotalBytes: 100, TotalSize: 100})
	callbacks.Status("Encrypted upload complete")
	got = output.String()
	if !strings.Contains(got, termui.Green) || !strings.Contains(got, "100% |") {
		t.Fatalf("stored completed bar is not green: %q", got)
	}
	if !strings.Contains(got, termui.Green+"Encrypted upload complete"+termui.Reset) {
		t.Fatalf("stored completion is not green: %q", got)
	}
}

func TestStoredCallbacksMeasureStyledUnicodeByDisplayWidth(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStyledStoredCallbacks(&output, false, true, "Uploading")
	callbacks.Status("Uploading 🎉")
	callbacks.Status("Done")

	wantClear := "\r" + strings.Repeat(" ", uniseg.StringWidth("Uploading 🎉")) + "\r"
	if got := output.String(); !strings.Contains(got, wantClear) {
		t.Fatalf("styled Unicode status did not clear by display width: %q", got)
	}
}

func TestStoredCallbacksUseAggregateMultiFileProgress(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, false, "Downloading")
	callbacks.Status("Downloading first.txt")
	callbacks.Progress(storeclient.Progress{
		FileName: "first.txt", FileCount: 2, TotalBytes: 25, TotalSize: 100,
	})
	callbacks.Status("Downloading second.txt")
	callbacks.Progress(storeclient.Progress{
		FileName: "second.txt", FileCount: 2, TotalBytes: 100, TotalSize: 100,
	})

	got := output.String()
	if !strings.Contains(got, "Downloading 2 files") || !strings.Contains(got, "100% |") {
		t.Fatalf("stored multi-file progress is not aggregate: %q", got)
	}
	if strings.Contains(got, "second.txt") {
		t.Fatalf("stored aggregate progress switched to a concurrent filename: %q", got)
	}
}

func TestFormatStoredSendInstructionsUsesRegularCrocPalette(t *testing.T) {
	plain := formatStoredSendInstructions(
		"tomorrow", "https://example.com/#secret", "croc-store-v1.token", "transfer-id", "one verified download", false,
	)
	colored := formatStoredSendInstructions(
		"tomorrow", "https://example.com/#secret", "croc-store-v1.token", "transfer-id", "one verified download", true,
	)
	if termui.Plain(colored) != plain {
		t.Fatalf("colored stored instructions changed text:\n%s", colored)
	}
	for _, secret := range []string{"https://example.com/#secret", "croc-store-v1.token", "transfer-id"} {
		if !strings.Contains(colored, termui.Yellow+secret+termui.Reset) {
			t.Fatalf("stored instructions do not highlight %q: %q", secret, colored)
		}
	}
	if !strings.Contains(colored, termui.Green+"Stored transfer is encrypted") {
		t.Fatalf("stored ready message is not green: %q", colored)
	}
}

func TestStoreCommandUploads(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command []string
		prefix  string
		custom  bool
		envURL  bool
	}{
		{name: "store defaults", command: []string{"store"}, envURL: true},
		{name: "store custom", command: []string{"store"}, custom: true},
		{name: "send defaults", command: []string{"send", "--store"}, prefix: "store-", envURL: true},
		{name: "send custom", command: []string{"send", "--store"}, prefix: "store-", custom: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("CROC_CONFIG_DIR", configDir)
			t.Setenv("CROC_DO_CHECK", "0")
			require.NoError(t, writeVersionCheckCache(filepath.Join(configDir, versionCheckCacheName), versionCheckCache{CheckedAt: time.Now(), LatestVersion: Version}))
			service, err := storeapi.New(storeapi.Config{
				Root: t.TempDir(), MaxTransferBytes: 8 << 20, MaxTotalBytes: 32 << 20,
				MinFreeBytes: 1, MaxDownloads: 10, CreatePerHour: 100, MaxActiveUploads: 10, DisableRootLock: true,
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, service.Close()) })
			type declaration struct {
				Downloads      int   `json:"downloads"`
				ExpiresSeconds int64 `json:"expiresSeconds"`
				Files          int   `json:"files"`
			}
			declarations := make(chan declaration, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/store/transfers" {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("read upload declaration: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var declared declaration
					if err := json.Unmarshal(body, &declared); err != nil {
						t.Errorf("decode upload declaration: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					declarations <- declared
				}
				service.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			args := append([]string{"croc", "--ignore-stdin", "--disable-clipboard", "--quiet"}, tt.command...)
			if tt.envURL {
				t.Setenv("CROC_STORE_URL", server.URL)
			} else {
				// An explicit URL must override the environment value.
				t.Setenv("CROC_STORE_URL", "http://127.0.0.1:1")
				args = append(args, "--"+tt.prefix+"url", server.URL)
			}
			wantDownloads, wantExpiration := 1, 24*time.Hour
			if tt.custom {
				args = append(args, "--"+tt.prefix+"downloads", "3", "--"+tt.prefix+"expiration", "3d")
				wantDownloads, wantExpiration = 3, 72*time.Hour
			}
			sourceDir := t.TempDir()
			for _, name := range []string{"first.txt", "second.txt"} {
				file := filepath.Join(sourceDir, name)
				require.NoError(t, os.WriteFile(file, []byte("stored upload fixture"), 0600))
				args = append(args, file)
			}
			started := time.Now()
			require.NoError(t, newApp().RunContext(t.Context(), args))
			select {
			case got := <-declarations:
				require.Equal(t, 2, got.Files)
				if got.Downloads == 0 {
					got.Downloads = 1
				}
				if got.ExpiresSeconds == 0 {
					got.ExpiresSeconds = int64(24 * time.Hour / time.Second)
				}
				require.Equal(t, wantDownloads, got.Downloads)
				require.Equal(t, int64(wantExpiration/time.Second), got.ExpiresSeconds)
			default:
				t.Fatal("command did not create a stored transfer")
			}
			receipts, err := readStoreReceipts()
			require.NoError(t, err)
			require.Len(t, receipts, 1)
			require.Equal(t, server.URL, receipts[0].Origin)
			require.WithinDuration(t, started.Add(wantExpiration), receipts[0].ExpiresAt, 5*time.Second)
		})
	}
}

func TestStoreCommandValidation(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CROC_CONFIG_DIR", configDir)
	t.Setenv("CROC_DO_CHECK", "0")
	require.NoError(t, writeVersionCheckCache(filepath.Join(configDir, versionCheckCacheName), versionCheckCache{CheckedAt: time.Now(), LatestVersion: Version}))
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"missing file", nil, "must specify file: croc store [filename(s)]"},
		{"zero downloads", []string{"--downloads=0", "unused-file"}, "--downloads must be positive"},
		{"negative downloads", []string{"--downloads=-1", "unused-file"}, "--downloads must be positive"},
		{"invalid expiration", []string{"--expiration=30s", "unused-file"}, "invalid --expiration:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"croc", "--ignore-stdin", "store"}, tt.args...)
			require.ErrorContains(t, newApp().Run(args), tt.want)
		})
	}
}
