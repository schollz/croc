package models

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestConstants(t *testing.T) {
	if TCP_BUFFER_SIZE != 1024*64 {
		t.Errorf("TCP_BUFFER_SIZE = %d, want %d", TCP_BUFFER_SIZE, 1024*64)
	}

	if DEFAULT_PORT != "9009" {
		t.Errorf("DEFAULT_PORT = %s, want %s", DEFAULT_PORT, "9009")
	}

	if DEFAULT_PASSPHRASE != "pass123" {
		t.Errorf("DEFAULT_PASSPHRASE = %s, want %s", DEFAULT_PASSPHRASE, "pass123")
	}
}

func TestPublicDNSServers(t *testing.T) {
	if len(publicDNS) == 0 {
		t.Error("publicDNS list should not be empty")
	}

	// Check that we have both IPv4 and IPv6 servers
	hasIPv4 := false
	hasIPv6 := false

	for _, dns := range publicDNS {
		if strings.Contains(dns, "[") {
			hasIPv6 = true
		} else {
			hasIPv4 = true
		}
	}

	if !hasIPv4 {
		t.Error("publicDNS should contain IPv4 servers")
	}

	if !hasIPv6 {
		t.Error("publicDNS should contain IPv6 servers")
	}

	// Verify known DNS servers are present
	expectedServers := []string{
		"1.1.1.1",        // Cloudflare
		"8.8.8.8",        // Google
		"9.9.9.9",        // Quad9
		"208.67.220.220", // OpenDNS
	}

	for _, expected := range expectedServers {
		found := false
		for _, dns := range publicDNS {
			if dns == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected DNS server %s not found in publicDNS", expected)
		}
	}
}

func TestLocalLookupIP(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "localhost",
			address: "localhost",
			wantErr: false,
		},
		{
			name:    "invalid hostname",
			address: "this-hostname-should-not-exist-12345",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := localLookupIP(tt.address)

			if (err != nil) != tt.wantErr {
				t.Errorf("localLookupIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && ip == "" {
				t.Error("localLookupIP() returned empty IP for valid hostname")
			}

			if !tt.wantErr {
				// Verify it's a valid IP address
				if net.ParseIP(ip) == nil {
					t.Errorf("localLookupIP() returned invalid IP: %s", ip)
				}
			}
		})
	}
}

func TestRemoteLookupIPTimeout(t *testing.T) {
	// Test with an invalid DNS server that should timeout
	start := time.Now()
	_, err := remoteLookupIP("example.com", "192.0.2.1")
	duration := time.Since(start)

	// Should timeout within reasonable time (we set 500ms timeout)
	if duration > time.Second {
		t.Errorf("remoteLookupIP took too long: %v", duration)
	}

	if err == nil {
		t.Error("remoteLookupIP should have failed with invalid DNS server")
	}
}

func TestLookupFunction(t *testing.T) {
	// Test the main lookup function
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "localhost",
			address: "localhost",
			wantErr: false,
		},
		{
			name:    "invalid hostname",
			address: "this-hostname-should-definitely-not-exist-98765",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := lookup(tt.address)

			if (err != nil) != tt.wantErr {
				t.Errorf("lookup() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && ip == "" {
				t.Error("lookup() returned empty IP for valid hostname")
			}

			if !tt.wantErr {
				// Verify it's a valid IP address
				if net.ParseIP(ip) == nil {
					t.Errorf("lookup() returned invalid IP: %s", ip)
				}
			}
		})
	}
}

func TestGetConfigFile(t *testing.T) {
	fname, err := getConfigFile(false)
	if err != nil {
		t.Skip("Could not get config directory")
	}

	if !strings.HasSuffix(fname, "internal-dns") {
		t.Errorf("Expected config file to end with 'internal-dns', got %s", fname)
	}
}
