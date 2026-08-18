package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/croc"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/publicrelay"
	"github.com/schollz/croc/v11/src/tcp"
)

func TestRelayMaxRoomsOpenConfiguration(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		unsetEnv(t, "CROC_MAX_ROOMS_OPEN")
		got := runRelayWithCapturedMaxRooms(t, []string{"croc", "relay"})
		if got != tcp.DEFAULT_MAX_ROOMS_OPEN {
			t.Fatalf("default max rooms open = %d, want %d", got, tcp.DEFAULT_MAX_ROOMS_OPEN)
		}
	})

	t.Run("flag", func(t *testing.T) {
		unsetEnv(t, "CROC_MAX_ROOMS_OPEN")
		got := runRelayWithCapturedMaxRooms(t, []string{"croc", "relay", "--max-rooms-open", "17"})
		if got != 17 {
			t.Fatalf("flag max rooms open = %d, want 17", got)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("CROC_MAX_ROOMS_OPEN", "23")
		got := runRelayWithCapturedMaxRooms(t, []string{"croc", "relay"})
		if got != 23 {
			t.Fatalf("environment max rooms open = %d, want 23", got)
		}
	})
}

func TestRelayRejectsNonPositiveMaxRoomsOpen(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			unsetEnv(t, "CROC_MAX_ROOMS_OPEN")
			app := newApp()
			err := app.Run([]string{"croc", "relay", "--max-rooms-open=" + value})
			if err == nil {
				t.Fatalf("--max-rooms-open=%s unexpectedly succeeded", value)
			}
			if got := err.Error(); got != "--max-rooms-open must be positive" {
				t.Fatalf("unexpected error: %q", got)
			}
		})
	}
}

func TestRelayHandshakeGuardConfiguration(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		unsetEnv(t, "CROC_MAX_PENDING_HANDSHAKES")
		unsetEnv(t, "CROC_HANDSHAKE_TIMEOUT")
		got := runRelayWithCapturedConfig(t, []string{"croc", "relay"})
		if got.maxPendingHandshakes != tcp.DEFAULT_MAX_PENDING_HANDSHAKES {
			t.Fatalf("default max pending handshakes = %d, want %d", got.maxPendingHandshakes, tcp.DEFAULT_MAX_PENDING_HANDSHAKES)
		}
		if got.handshakeTimeout != tcp.DEFAULT_HANDSHAKE_TIMEOUT {
			t.Fatalf("default handshake timeout = %s, want %s", got.handshakeTimeout, tcp.DEFAULT_HANDSHAKE_TIMEOUT)
		}
	})

	t.Run("flags", func(t *testing.T) {
		unsetEnv(t, "CROC_MAX_PENDING_HANDSHAKES")
		unsetEnv(t, "CROC_HANDSHAKE_TIMEOUT")
		got := runRelayWithCapturedConfig(t, []string{
			"croc", "relay",
			"--max-pending-handshakes", "19",
			"--handshake-timeout", "7m",
		})
		if got.maxPendingHandshakes != 19 {
			t.Fatalf("max pending handshakes = %d, want 19", got.maxPendingHandshakes)
		}
		if got.handshakeTimeout != 7*time.Minute {
			t.Fatalf("handshake timeout = %s, want 7m", got.handshakeTimeout)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("CROC_MAX_PENDING_HANDSHAKES", "23")
		t.Setenv("CROC_HANDSHAKE_TIMEOUT", "8m")
		got := runRelayWithCapturedConfig(t, []string{"croc", "relay"})
		if got.maxPendingHandshakes != 23 {
			t.Fatalf("max pending handshakes = %d, want 23", got.maxPendingHandshakes)
		}
		if got.handshakeTimeout != 8*time.Minute {
			t.Fatalf("handshake timeout = %s, want 8m", got.handshakeTimeout)
		}
	})
}

func TestRelayRejectsNonPositiveHandshakeGuards(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "zero pending", arg: "--max-pending-handshakes=0", want: "--max-pending-handshakes must be positive"},
		{name: "negative pending", arg: "--max-pending-handshakes=-1", want: "--max-pending-handshakes must be positive"},
		{name: "zero timeout", arg: "--handshake-timeout=0s", want: "--handshake-timeout must be positive"},
		{name: "negative timeout", arg: "--handshake-timeout=-1s", want: "--handshake-timeout must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "CROC_MAX_PENDING_HANDSHAKES")
			unsetEnv(t, "CROC_HANDSHAKE_TIMEOUT")
			err := newApp().Run([]string{"croc", "relay", tt.arg})
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded", tt.arg)
			}
			if err.Error() != tt.want {
				t.Fatalf("unexpected error: %q", err)
			}
		})
	}
}

func runRelayWithCapturedMaxRooms(t *testing.T, args []string) int {
	t.Helper()
	return runRelayWithCapturedConfig(t, args).maxRoomsOpen
}

type capturedRelayConfig struct {
	maxRoomsOpen         int
	maxPendingHandshakes int
	handshakeTimeout     time.Duration
}

func runRelayWithCapturedConfig(t *testing.T, args []string) capturedRelayConfig {
	t.Helper()
	app := newApp()
	var got capturedRelayConfig
	for _, command := range app.Commands {
		if command.Name != "relay" {
			continue
		}
		command.Action = func(ctx *cli.Context) error {
			got.maxRoomsOpen = ctx.Int("max-rooms-open")
			got.maxPendingHandshakes = ctx.Int("max-pending-handshakes")
			got.handshakeTimeout = ctx.Duration("handshake-timeout")
			return nil
		}
		if err := app.Run(args); err != nil {
			t.Fatalf("parse relay configuration: %v", err)
		}
		return got
	}
	t.Fatal("relay command not found")
	return capturedRelayConfig{}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	oldValue, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			if err := os.Setenv(key, oldValue); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
			return
		}
		if err := os.Unsetenv(key); err != nil {
			t.Errorf("clear %s: %v", key, err)
		}
	})
}

func TestWritePrivateConfigFile(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
	}{
		{name: "new config"},
		{name: "existing permissive config", existing: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.json")
			if tt.existing {
				if err := os.WriteFile(configPath, []byte("old secret"), 0o644); err != nil {
					t.Fatalf("create existing config: %v", err)
				}
				if err := os.Chmod(configPath, 0o644); err != nil {
					t.Fatalf("make existing config permissive: %v", err)
				}
			}

			want := []byte(`{"RelayPassword":"new secret"}`)
			if err := writePrivateConfigFile(configPath, want); err != nil {
				t.Fatalf("write private config: %v", err)
			}

			got, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("config contents = %q, want %q", got, want)
			}

			if runtime.GOOS != "windows" {
				info, err := os.Stat(configPath)
				if err != nil {
					t.Fatalf("stat config: %v", err)
				}
				if gotMode := info.Mode().Perm(); gotMode != 0o600 {
					t.Fatalf("config permissions = %o, want 600", gotMode)
				}
			}
		})
	}
}

// determinePass should trim a pass from --pass/CROC_PASS, not just from a file.
func TestDeterminePassTrimsEnvValue(t *testing.T) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("pass", "", "")
	if err := set.Set("pass", "pass123\n"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	ctx := cli.NewContext(nil, set, nil)

	got := determinePass(ctx)
	want := "pass123"
	if got != want {
		t.Errorf("determinePass(%q) = %q, want %q", "pass123\\n", got, want)
	}
}

func TestRevokeIsRootFlag(t *testing.T) {
	app := newApp()
	var revoke string
	app.Action = func(ctx *cli.Context) error {
		revoke = ctx.String("revoke")
		return nil
	}

	if err := app.Run([]string{"croc", "--revoke", "transfer-id"}); err != nil {
		t.Fatalf("parse --revoke: %v", err)
	}
	if revoke != "transfer-id" {
		t.Fatalf("--revoke = %q, want transfer-id", revoke)
	}
	for _, command := range app.Commands {
		if command.Name == "revoke" {
			t.Fatal("revoke should not be registered as a subcommand")
		}
	}
}

func TestStoreDownloadsFlagParsing(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want int
	}{
		{name: "default", args: []string{"croc", "send"}, want: 1},
		{name: "custom", args: []string{"croc", "send", "--store-downloads", "4"}, want: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app := newApp()
			for _, command := range app.Commands {
				if command.Name != "send" {
					continue
				}
				var got int
				command.Action = func(ctx *cli.Context) error {
					got = ctx.Int("store-downloads")
					return nil
				}
				if err := app.Run(testCase.args); err != nil {
					t.Fatalf("parse store downloads: %v", err)
				}
				if got != testCase.want {
					t.Fatalf("store downloads = %d, want %d", got, testCase.want)
				}
				return
			}
			t.Fatal("send command not found")
		})
	}
}

func TestStoreExpirationFlagParsing(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default", args: []string{"croc", "send"}, want: "1d"},
		{name: "custom", args: []string{"croc", "send", "--store-expiration", "3d"}, want: "3d"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app := newApp()
			for _, command := range app.Commands {
				if command.Name != "send" {
					continue
				}
				var got string
				command.Action = func(ctx *cli.Context) error {
					got = ctx.String("store-expiration")
					return nil
				}
				if err := app.Run(testCase.args); err != nil {
					t.Fatalf("parse store expiration: %v", err)
				}
				if got != testCase.want {
					t.Fatalf("store expiration = %q, want %q", got, testCase.want)
				}
				return
			}
			t.Fatal("send command not found")
		})
	}
}

func TestStoredSendRejectsInvalidExpiration(t *testing.T) {
	err := newApp().Run([]string{
		"croc",
		"--ignore-stdin",
		"send",
		"--store",
		"--store-expiration=30s",
		"unused-file",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid --store-expiration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoredSendRejectsNonPositiveDownloadCount(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			err := newApp().Run([]string{
				"croc",
				"--ignore-stdin",
				"send",
				"--store",
				"--store-downloads=" + value,
				"unused-file",
			})
			if err == nil || err.Error() != "--store-downloads must be positive" {
				t.Fatalf("unexpected error for %s downloads: %v", value, err)
			}
		})
	}
}

func TestServeIsNotRegistered(t *testing.T) {
	for _, command := range newApp().Commands {
		if command.Name == "serve" {
			t.Fatal("serve belongs to the standalone croc-web binary")
		}
	}
	if err := newApp().Run([]string{"croc", "serve"}); err == nil || err.Error() != "the web server has moved to the standalone croc-web binary" {
		t.Fatalf("croc serve error = %v", err)
	}
}

func TestParseRelayPorts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "plain comma separated",
			in:   "9009,9010,9011",
			want: []string{"9009", "9010", "9011"},
		},
		{
			name: "spaces after commas are trimmed",
			in:   "9009, 9010, 9011",
			want: []string{"9009", "9010", "9011"},
		},
		{
			name: "surrounding and trailing empties are dropped",
			in:   " 9009 ,, 9010 ,",
			want: []string{"9009", "9010"},
		},
		{
			name: "single port",
			in:   "9009",
			want: []string{"9009"},
		},
		{
			name: "empty string yields no ports",
			in:   "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRelayPorts(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseRelayPorts(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveSendSharedSecret(t *testing.T) {
	t.Run("uses env secret", func(t *testing.T) {
		got := resolveSendSharedSecret("generated-secret", "password-example")
		if got != "password-example" {
			t.Fatalf("expected env secret, got %q", got)
		}
	})

	t.Run("falls back to existing secret when env is empty", func(t *testing.T) {
		got := resolveSendSharedSecret("generated-secret", "")
		if got != "generated-secret" {
			t.Fatalf("expected existing secret, got %q", got)
		}
	})
}

func TestUsesPublicRelayOnlyForUnmodifiedDefaults(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		mutate     func(*croc.Options)
		wantPublic bool
	}{
		{name: "defaults", args: []string{"croc", "send"}, wantPublic: true},
		{name: "explicit relay", args: []string{"croc", "--relay", "relay.example:9009", "send"}},
		{name: "explicit relay6", args: []string{"croc", "--relay6", "[::1]:9009", "send"}},
		{name: "local only", args: []string{"croc", "--local", "send"}},
		{
			name: "remembered custom relay",
			args: []string{"croc", "send"},
			mutate: func(options *croc.Options) {
				options.RelayAddress = "remembered.example:9009"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newApp()
			for _, command := range app.Commands {
				if command.Name != "send" {
					continue
				}
				var got bool
				command.Action = func(ctx *cli.Context) error {
					options := croc.Options{
						RelayAddress:  ctx.String("relay"),
						RelayAddress6: ctx.String("relay6"),
						OnlyLocal:     ctx.Bool("local"),
					}
					if tt.mutate != nil {
						tt.mutate(&options)
					}
					got = usesPublicRelay(ctx, options)
					return nil
				}
				if err := app.Run(tt.args); err != nil {
					t.Fatal(err)
				}
				if got != tt.wantPublic {
					t.Fatalf("usesPublicRelay = %v, want %v", got, tt.wantPublic)
				}
				return
			}
			t.Fatal("send command not found")
		})
	}
}

func TestAssignPublicRelayForCode(t *testing.T) {
	tests := map[string]string{
		"Word-word-word":    "1.getcroc.com:9009",
		"acid-acorn-acre":   "2.getcroc.com:9009",
		"poker-hedge-floss": "3.getcroc.com:9009",
	}
	for code, address := range tests {
		options := croc.Options{SharedSecret: code, RelayAddress6: models.DEFAULT_RELAY6}
		if err := assignPublicRelayForCode(&options); err != nil {
			t.Fatalf("assign %q: %v", code, err)
		}
		if options.RelayAddress != address || options.RelayAddress6 != "" || !options.PublicRelay {
			t.Fatalf("assign %q = %#v", code, options)
		}
	}
}

func TestSelectBestPublicRelay(t *testing.T) {
	probe := func(ctx context.Context, address string, _ time.Duration) (time.Duration, error) {
		switch address {
		case publicrelay.Relays()[0]:
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(30 * time.Millisecond):
				return 30 * time.Millisecond, nil
			}
		case publicrelay.Relays()[1]:
			time.Sleep(10 * time.Millisecond)
			return 10 * time.Millisecond, nil
		default:
			return 0, errors.New("unavailable")
		}
	}
	index, err := selectBestPublicRelay(probe)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("best relay index = %d, want 1", index)
	}
}

func TestBestPublicRelayCacheUsesAddressAndCurrentPoolOrder(t *testing.T) {
	t.Setenv("CROC_CONFIG_DIR", t.TempDir())
	relays := []string{"one:9009", "two:9009", "three:9009"}
	if err := saveBestPublicRelay("two:9009"); err != nil {
		t.Fatal(err)
	}
	index, err := loadBestPublicRelay(relays)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("cached relay index = %d, want 1", index)
	}
	index, err = loadBestPublicRelay([]string{"three:9009", "one:9009", "two:9009"})
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 {
		t.Fatalf("reordered cached relay index = %d, want 2", index)
	}
	cacheFile, err := getBestRelayCacheFile(false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache permissions = %o, want 600", got)
	}
}

func TestSelectPublicRelayCacheHitBypassesProbes(t *testing.T) {
	t.Setenv("CROC_CONFIG_DIR", t.TempDir())
	relays := publicrelay.Relays()
	if err := saveBestPublicRelay(relays[2]); err != nil {
		t.Fatal(err)
	}
	probes := 0
	index, err := selectPublicRelay(func(context.Context, string, time.Duration) (time.Duration, error) {
		probes++
		return 0, errors.New("probe should not run")
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 || probes != 0 {
		t.Fatalf("selection = (%d, %d probes), want (2, 0)", index, probes)
	}
}

func TestSelectPublicRelayReplacesInvalidCache(t *testing.T) {
	for _, cached := range []string{"", "unknown:9009", " 2.getcroc.com:9009"} {
		t.Run(fmt.Sprintf("cached_%q", cached), func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("CROC_CONFIG_DIR", configDir)
			if err := os.WriteFile(filepath.Join(configDir, "best-relay"), []byte(cached), 0o600); err != nil {
				t.Fatal(err)
			}
			winner := publicrelay.Relays()[1]
			index, err := selectPublicRelay(func(ctx context.Context, address string, _ time.Duration) (time.Duration, error) {
				if address == winner {
					return time.Millisecond, nil
				}
				<-ctx.Done()
				return 0, ctx.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
			if index != 1 {
				t.Fatalf("selected index = %d, want 1", index)
			}
			contents, err := os.ReadFile(filepath.Join(configDir, "best-relay"))
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != winner {
				t.Fatalf("cache = %q, want %q", contents, winner)
			}
		})
	}
}

func TestSelectPublicRelayIgnoresCacheWriteFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(configPath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CROC_CONFIG_DIR", configPath)
	winner := publicrelay.Relays()[0]
	index, err := selectPublicRelay(func(ctx context.Context, address string, _ time.Duration) (time.Duration, error) {
		if address == winner {
			return time.Millisecond, nil
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatalf("selected index = %d, want 0", index)
	}
}

func TestRelayConnectionErrorClearsOnlyGeneratedPublicCache(t *testing.T) {
	tests := []struct {
		name      string
		generated bool
		err       error
		wantCache bool
	}{
		{name: "relay failure", generated: true, err: fmt.Errorf("send: %w", croc.ErrRelayConnection)},
		{name: "peer failure", generated: true, err: errors.New("peer disconnected"), wantCache: true},
		{name: "custom code", err: croc.ErrRelayConnection, wantCache: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CROC_CONFIG_DIR", t.TempDir())
			if err := saveBestPublicRelay(publicrelay.Relays()[0]); err != nil {
				t.Fatal(err)
			}
			clearBestPublicRelayOnSendError(test.generated, test.err)
			_, err := os.Stat(filepath.Join(os.Getenv("CROC_CONFIG_DIR"), "best-relay"))
			if test.wantCache && err != nil {
				t.Fatalf("cache should remain: %v", err)
			}
			if !test.wantCache && !os.IsNotExist(err) {
				t.Fatalf("cache should be removed, stat error = %v", err)
			}
		})
	}
}

func TestShouldExitForUnixSendCode(t *testing.T) {
	tests := []struct {
		name                string
		goos                string
		codeFlagSet         bool
		classicInsecureMode bool
		envSecret           string
		want                bool
	}{
		{
			name:                "unix with code flag and no env exits",
			goos:                "linux",
			codeFlagSet:         true,
			classicInsecureMode: false,
			envSecret:           "",
			want:                true,
		},
		{
			name:                "unix with env set does not exit",
			goos:                "linux",
			codeFlagSet:         true,
			classicInsecureMode: false,
			envSecret:           "password-example",
			want:                false,
		},
		{
			name:                "classic mode does not exit",
			goos:                "linux",
			codeFlagSet:         true,
			classicInsecureMode: true,
			envSecret:           "",
			want:                false,
		},
		{
			name:                "windows does not exit",
			goos:                "windows",
			codeFlagSet:         true,
			classicInsecureMode: false,
			envSecret:           "",
			want:                false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExitForUnixSendCode(tt.goos, tt.codeFlagSet, tt.classicInsecureMode, tt.envSecret)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestApplyRememberedSendOptionsDisableClipboard(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		remembered bool
		want       bool
	}{
		{
			name:       "inherits remembered true",
			args:       []string{"croc", "send"},
			remembered: true,
			want:       true,
		},
		{
			name:       "inherits remembered false",
			args:       []string{"croc", "send"},
			remembered: false,
			want:       false,
		},
		{
			name:       "explicit false overrides remembered true",
			args:       []string{"croc", "--disable-clipboard=false", "send"},
			remembered: true,
			want:       false,
		},
		{
			name:       "explicit true overrides remembered false",
			args:       []string{"croc", "--disable-clipboard", "send"},
			remembered: false,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newApp()
			for _, command := range app.Commands {
				if command.Name != "send" {
					continue
				}
				var got croc.Options
				command.Action = func(ctx *cli.Context) error {
					got.DisableClipboard = ctx.Bool("disable-clipboard")
					applyRememberedSendOptions(ctx, &got, croc.Options{DisableClipboard: tt.remembered})
					return nil
				}
				if err := app.Run(tt.args); err != nil {
					t.Fatalf("apply remembered send options: %v", err)
				}
				if got.DisableClipboard != tt.want {
					t.Fatalf("DisableClipboard = %v, want %v", got.DisableClipboard, tt.want)
				}
				return
			}
			t.Fatal("send command not found")
		})
	}
}
