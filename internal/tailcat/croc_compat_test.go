package tailcat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go4.org/mem"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/wgcfg"
)

func TestConnBlobLegacyEmbeddedRegion(t *testing.T) {
	// Captured from TestConnBlob/remove_implicit_fields_on_marshal before
	// upgrading tailscale.com from v1.102.5. Keep the historical encoding.
	const legacy ConnBlob = "tcomFwWCAAAQIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAH2FygaFhToKhYWhsdGMxYS5pcG4uZGV2oWFodGRlcnAxYi50YWlsc2NhbGUuY29t"
	raw := [32]byte{1: 1, 2: 2, 31: 31}
	want := ConnInfo{
		ServerPublic: NodePublic{key.NodePublicFromRaw32(mem.B(raw[:]))},
		Region: []*tailcfg.DERPRegion{{
			RegionID:   1,
			RegionCode: "1",
			Nodes: []*tailcfg.DERPNode{
				{Name: "tc1a.ipn.dev", RegionID: 1, HostName: "tc1a.ipn.dev"},
				{Name: "derp1b.tailscale.com", RegionID: 1, HostName: "derp1b.tailscale.com"},
			},
		}},
	}
	got, err := ParseConnBlob(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("legacy blob decoded differently (-want +got):\n%s", diff)
	}
	if encoded := got.ConnBlob(); encoded != legacy {
		t.Fatalf("legacy blob re-encoded as %q; want %q", encoded, legacy)
	}
}

func TestConnBlobRegionIDs(t *testing.T) {
	for _, id := range []tailcfg.DERPRegionID{-1, 0, 10, 1<<32 + 7, 1<<53 - 1} {
		t.Run(id.String(), func(t *testing.T) {
			ci := ConnInfo{RegionID: id}
			got, err := ParseConnBlob(ci.ConnBlob())
			if err != nil {
				t.Fatal(err)
			}
			if got.RegionID != id {
				t.Fatalf("region ID = %v; want %v", got.RegionID, id)
			}
			raw, err := ParseConnBlobRaw(ci.ConnBlob())
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{"connection": got, "wire": raw} {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var numeric struct{ RegionID int64 }
				if err := json.Unmarshal(data, &numeric); err != nil {
					t.Fatalf("%s region ID is not a JSON integer: %s: %v", name, data, err)
				}
				if numeric.RegionID != id.Int64() {
					t.Fatalf("%s JSON region ID = %v; want %v", name, numeric.RegionID, id)
				}
			}
		})
	}
}

func TestWireRegionIDWidth(t *testing.T) {
	const id tailcfg.DERPRegionID = 1<<53 - 1
	want := &tailcfg.DERPRegion{
		RegionID: id,
		Nodes: []*tailcfg.DERPNode{{
			RegionID: id,
			HostName: "relay.example.com",
		}},
	}
	encoded, err := cbor.Marshal(wireRegionOf(want))
	if err != nil {
		t.Fatal(err)
	}
	var decoded wireRegion
	if err := cbor.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, decoded.derpRegion()); diff != "" {
		t.Fatalf("region IDs truncated during wire round trip (-want +got):\n%s", diff)
	}
}

func TestExpandWideDERPRegion(t *testing.T) {
	const id tailcfg.DERPRegionID = 1<<32 + 7
	data := fmt.Appendf(nil, `{"Regions":{"%d":{"RegionID":%d,"Nodes":[{"RegionID":%d,"HostName":"relay.example.com"}]}}}`, id, id, id)
	dm := decodeDERPMap(data)
	if dm == nil {
		t.Fatal("failed to decode DERP map")
	}
	ci := ConnInfo{RegionID: id}
	if err := ci.Expand(context.Background(), dm); err != nil {
		t.Fatal(err)
	}
	if len(ci.Region) != 1 || ci.Region[0].RegionID != id || ci.Region[0].Nodes[0].RegionID != id {
		t.Fatalf("expanded region IDs truncated: %+v", ci.Region)
	}
}

func TestPeerConfig(t *testing.T) {
	known := key.NewNode().Public()
	unknown := key.NewNode().Public()
	prefix := pfxOf(tcAddrForKey(known))
	client := &locoBackend{serverPub: known}
	server := &locoBackend{clients: map[key.NodePublic]*tailcfg.Node{
		known: {AllowedIPs: []netip.Prefix{prefix}},
	}}
	for _, tt := range []struct {
		name    string
		backend *locoBackend
		peer    key.NodePublic
		want    wgcfg.PeerConfig
		ok      bool
	}{
		{"known_server", client, known, wgcfg.PeerConfig{AllowedIPs: []netip.Prefix{prefix, allIPv6}}, true},
		{"unknown_server", client, unknown, wgcfg.PeerConfig{}, false},
		{"known_client", server, known, wgcfg.PeerConfig{AllowedIPs: []netip.Prefix{prefix}}, true},
		{"unknown_client", server, unknown, wgcfg.PeerConfig{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.backend.peerConfig(tt.peer)
			if ok != tt.ok {
				t.Fatalf("peer known = %v; want %v", ok, tt.ok)
			}
			// Comparing the complete config also checks that PresharedKey
			// stays zero, preserving the existing tunnel protocol.
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateComparable(netip.Prefix{})); diff != "" {
				t.Fatalf("peer configuration changed (-want +got):\n%s", diff)
			}
		})
	}
}
