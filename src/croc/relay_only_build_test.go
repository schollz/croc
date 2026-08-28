//go:build croc_no_tailcat

package croc

import "testing"

func TestRelayOnlyBuildResolvesStrictTailcatToRelay(t *testing.T) {
	mode, downgraded, err := ResolveTransportMode(string(TransportDERP))
	if err != nil {
		t.Fatal(err)
	}
	if mode != TransportRelay || !downgraded {
		t.Fatalf("ResolveTransportMode(derp) = (%q, %t); want (relay, true)", mode, downgraded)
	}

	client, err := New(Options{
		SharedSecret: "relay-only-build",
		IsSender:     true,
		Transport:    TransportDERP,
		ShowQrCode:   true,
		Curve:        "p256",
	})
	if err != nil {
		t.Fatalf("create relay-only client: %v", err)
	}
	if client.Options.Transport != TransportRelay {
		t.Fatalf("client transport = %q; want relay", client.Options.Transport)
	}
	if supportsFeature(client.pakeFeatures(), tailcatFeature) {
		t.Fatal("relay-only client advertised Tailcat")
	}
}
