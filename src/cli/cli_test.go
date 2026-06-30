package cli

import (
	"reflect"
	"testing"
)

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

func TestShouldExitForUnixSendCode(t *testing.T) {
	tests := []struct {
		name               string
		goos               string
		codeFlagSet        bool
		classicInsecureMode bool
		envSecret          string
		want               bool
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
