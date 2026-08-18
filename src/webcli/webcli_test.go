package webcli

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	internalcli "github.com/schollz/croc/v11/internal/cli"
)

func TestAppIdentityAndArguments(t *testing.T) {
	app := newApp(context.Background())
	if app.Name != "croc-web" {
		t.Fatalf("app name = %q, want croc-web", app.Name)
	}
	if err := app.Run([]string{"croc-web", "one.example", "two.example"}); err == nil {
		t.Fatal("multiple public addresses unexpectedly succeeded")
	}
}

func TestStoreDownloadsFlagParsing(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want int
	}{
		{name: "default", args: []string{"croc-web"}, want: 1},
		{name: "custom", args: []string{"croc-web", "--store-downloads", "6"}, want: 6},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("CROC_STORE_DOWNLOADS", "")
			app := newApp(context.Background())
			var got int
			app.Action = func(ctx *internalcli.Context) error {
				got = ctx.Int("store-downloads")
				return nil
			}
			if err := app.Run(testCase.args); err != nil {
				t.Fatalf("parse store downloads: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("store downloads = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestStoreDownloadsEnvironmentConfiguration(t *testing.T) {
	t.Setenv("CROC_STORE_DOWNLOADS", "12")
	app := newApp(context.Background())
	var got int
	app.Action = func(ctx *internalcli.Context) error {
		got = ctx.Int("store-downloads")
		return nil
	}
	if err := app.Run([]string{"croc-web"}); err != nil {
		t.Fatalf("parse store downloads environment: %v", err)
	}
	if got != 12 {
		t.Fatalf("store downloads = %d, want 12", got)
	}
}

func TestStoreDownloadsMustBePositiveWhenStorageIsEnabled(t *testing.T) {
	err := newApp(context.Background()).Run([]string{
		"croc-web",
		"--store-dir", t.TempDir(),
		"--store-downloads", "0",
	})
	if err == nil || err.Error() != "--store-downloads must be positive" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreMaxExpirationConfiguration(t *testing.T) {
	t.Run("default is unlimited", func(t *testing.T) {
		t.Setenv("CROC_STORE_MAX_EXPIRATION", "")
		if err := os.Unsetenv("CROC_STORE_MAX_EXPIRATION"); err != nil {
			t.Fatal(err)
		}
		app := newApp(context.Background())
		var got string
		app.Action = func(ctx *internalcli.Context) error {
			got = ctx.String("store-max-expiration")
			return nil
		}
		if err := app.Run([]string{"croc-web"}); err != nil {
			t.Fatal(err)
		}
		if got != "0" {
			t.Fatalf("store max expiration = %q, want 0", got)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("CROC_STORE_MAX_EXPIRATION", "2w")
		app := newApp(context.Background())
		var got string
		app.Action = func(ctx *internalcli.Context) error {
			got = ctx.String("store-max-expiration")
			return nil
		}
		if err := app.Run([]string{"croc-web"}); err != nil {
			t.Fatal(err)
		}
		if got != "2w" {
			t.Fatalf("store max expiration = %q, want 2w", got)
		}
	})

	t.Run("flag overrides environment", func(t *testing.T) {
		t.Setenv("CROC_STORE_MAX_EXPIRATION", "2w")
		app := newApp(context.Background())
		var got string
		app.Action = func(ctx *internalcli.Context) error {
			got = ctx.String("store-max-expiration")
			return nil
		}
		if err := app.Run([]string{"croc-web", "--store-max-expiration", "3d"}); err != nil {
			t.Fatal(err)
		}
		if got != "3d" {
			t.Fatalf("store max expiration = %q, want 3d", got)
		}
	})
}

func TestStoreMaxExpirationValidation(t *testing.T) {
	for _, value := range []string{"30s", "0m", "-1h"} {
		t.Run(value, func(t *testing.T) {
			err := newApp(context.Background()).Run([]string{
				"croc-web",
				"--store-dir", t.TempDir(),
				"--store-max-expiration", value,
			})
			if err == nil || !strings.Contains(err.Error(), "invalid --store-max-expiration") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseRelayPorts(t *testing.T) {
	tests := map[string][]string{
		"9009,9010,9011":   {"9009", "9010", "9011"},
		"9009, 9010, 9011": {"9009", "9010", "9011"},
		" 9009 ,, 9010 ,":  {"9009", "9010"},
		"9009":             {"9009"},
		"":                 nil,
	}
	for input, expected := range tests {
		if actual := parseRelayPorts(input); !reflect.DeepEqual(actual, expected) {
			t.Fatalf("parseRelayPorts(%q) = %#v, want %#v", input, actual, expected)
		}
	}
}

func TestParseRelayHosts(t *testing.T) {
	assertions := map[string][]string{
		"1.getcroc.com,2.getcroc.com,3.getcroc.com": {"1.getcroc.com", "2.getcroc.com", "3.getcroc.com"},
		" relay.example ,, backup.example ":         {"relay.example", "backup.example"},
		"":                                          nil,
	}
	for input, expected := range assertions {
		if actual := parseRelayHosts(input); !reflect.DeepEqual(actual, expected) {
			t.Fatalf("parseRelayHosts(%q) = %#v, want %#v", input, actual, expected)
		}
	}
}

func TestResolveServeAddress(t *testing.T) {
	tests := []struct {
		name          string
		publicAddress string
		bindAddress   string
		bindExplicit  bool
		wantBind      string
		wantOrigin    string
		wantError     bool
	}{
		{name: "defaults", bindAddress: "127.0.0.1:9014", wantBind: "127.0.0.1:9014", wantOrigin: "localhost:5173"},
		{name: "public host uses default bind", publicAddress: "getcroc.com", bindAddress: "127.0.0.1:9014", wantBind: "127.0.0.1:9014", wantOrigin: "getcroc.com"},
		{name: "local development host binds directly", publicAddress: "localhost:5173", bindAddress: "127.0.0.1:9014", wantBind: "localhost:5173", wantOrigin: "localhost:5173"},
		{name: "explicit bind wins for local host", publicAddress: "localhost:5173", bindAddress: "0.0.0.0:8080", bindExplicit: true, wantBind: "0.0.0.0:8080", wantOrigin: "localhost:5173"},
		{name: "loopback IP binds directly", publicAddress: "127.0.0.1:7000", bindAddress: "127.0.0.1:9014", wantBind: "127.0.0.1:7000", wantOrigin: "127.0.0.1:7000"},
		{name: "rejects URL", publicAddress: "https://getcroc.com", bindAddress: "127.0.0.1:9014", wantError: true},
		{name: "rejects invalid public port", publicAddress: "localhost:not-a-port", bindAddress: "127.0.0.1:9014", wantError: true},
		{name: "rejects invalid bind", publicAddress: "getcroc.com", bindAddress: "localhost", bindExplicit: true, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBind, gotOrigin, err := resolveServeAddress(tt.publicAddress, tt.bindAddress, tt.bindExplicit)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotBind != tt.wantBind {
				t.Fatalf("bind = %q, want %q", gotBind, tt.wantBind)
			}
			if gotOrigin != tt.wantOrigin {
				t.Fatalf("origin = %q, want %q", gotOrigin, tt.wantOrigin)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	tests := map[string]int64{
		"1":      1,
		"512MiB": 512 << 20,
		"5GB":    5_000_000_000,
		"2 GiB":  2 << 30,
	}
	for input, expected := range tests {
		actual, err := parseByteSize(input)
		if err != nil {
			t.Fatalf("parseByteSize(%q): %v", input, err)
		}
		if actual != expected {
			t.Fatalf("parseByteSize(%q) = %d, want %d", input, actual, expected)
		}
	}
	for _, input := range []string{"", "-1", "1.5GiB", "999999999999999999999TiB"} {
		if _, err := parseByteSize(input); err == nil {
			t.Fatalf("parseByteSize(%q) unexpectedly succeeded", input)
		}
	}
}

func TestDeterminePassReadsFilesAndTrims(t *testing.T) {
	path := t.TempDir() + "/pass"
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if actual := determinePass(path); actual != "secret" {
		t.Fatalf("password = %q, want secret", actual)
	}
}
