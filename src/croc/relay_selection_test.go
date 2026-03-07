package croc

import "testing"

func TestSplitRelayAddress(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		fallbackPort string
		wantHost     string
		wantPort     string
		wantErr      bool
	}{
		{
			name:         "ipv4 hostport",
			input:        "203.0.113.10:9009",
			fallbackPort: "9009",
			wantHost:     "203.0.113.10",
			wantPort:     "9009",
		},
		{
			name:         "ipv6 hostport",
			input:        "[2001:db8::1]:9010",
			fallbackPort: "9009",
			wantHost:     "2001:db8::1",
			wantPort:     "9010",
		},
		{
			name:         "ipv6 without port",
			input:        "2001:db8::1",
			fallbackPort: "9009",
			wantHost:     "2001:db8::1",
			wantPort:     "9009",
		},
		{
			name:         "hostname without port",
			input:        "relay.example.com",
			fallbackPort: "9009",
			wantHost:     "relay.example.com",
			wantPort:     "9009",
		},
		{
			name:         "empty address",
			input:        "",
			fallbackPort: "9009",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := splitRelayAddress(tt.input, tt.fallbackPort)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost {
				t.Fatalf("host mismatch: got %q want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Fatalf("port mismatch: got %q want %q", port, tt.wantPort)
			}
		})
	}
}
