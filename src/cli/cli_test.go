package cli

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/schollz/cli/v2"
	"github.com/schollz/croc/v10/src/tcp"
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
		{
			name:          "defaults",
			publicAddress: "",
			bindAddress:   "127.0.0.1:9014",
			wantBind:      "127.0.0.1:9014",
			wantOrigin:    "localhost:5173",
		},
		{
			name:          "public host uses default bind",
			publicAddress: "getcroc.com",
			bindAddress:   "127.0.0.1:9014",
			wantBind:      "127.0.0.1:9014",
			wantOrigin:    "getcroc.com",
		},
		{
			name:          "local development host binds directly",
			publicAddress: "localhost:5173",
			bindAddress:   "127.0.0.1:9014",
			wantBind:      "localhost:5173",
			wantOrigin:    "localhost:5173",
		},
		{
			name:          "explicit bind wins for local host",
			publicAddress: "localhost:5173",
			bindAddress:   "0.0.0.0:8080",
			bindExplicit:  true,
			wantBind:      "0.0.0.0:8080",
			wantOrigin:    "localhost:5173",
		},
		{
			name:          "loopback IP binds directly",
			publicAddress: "127.0.0.1:7000",
			bindAddress:   "127.0.0.1:9014",
			wantBind:      "127.0.0.1:7000",
			wantOrigin:    "127.0.0.1:7000",
		},
		{
			name:          "rejects URL",
			publicAddress: "https://getcroc.com",
			bindAddress:   "127.0.0.1:9014",
			wantError:     true,
		},
		{
			name:          "rejects invalid public port",
			publicAddress: "localhost:not-a-port",
			bindAddress:   "127.0.0.1:9014",
			wantError:     true,
		},
		{
			name:          "rejects invalid bind",
			publicAddress: "getcroc.com",
			bindAddress:   "localhost",
			bindExplicit:  true,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBind, gotOrigin, err := resolveServeAddress(
				tt.publicAddress,
				tt.bindAddress,
				tt.bindExplicit,
			)
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
