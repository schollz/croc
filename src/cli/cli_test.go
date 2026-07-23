package cli

import (
	"flag"
	"reflect"
	"testing"

	"github.com/schollz/cli/v2"
)

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
			publicAddress: "share.schollz.com",
			bindAddress:   "127.0.0.1:9014",
			wantBind:      "127.0.0.1:9014",
			wantOrigin:    "share.schollz.com",
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
			publicAddress: "https://share.schollz.com",
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
			publicAddress: "share.schollz.com",
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
