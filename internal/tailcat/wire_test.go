// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailcat

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"tailscale.com/tailcfg"
)

// wireFieldNames maps the short CBOR field names used by the wire
// types in wire.go to their Go field names. Every short name is
// globally unique across all wire types, and every Go field name has
// exactly one short name.
//
// Do not change or reuse existing entries: the short names are the
// ConnBlob wire format.
var wireFieldNames = map[string]string{
	"p": "ServerPublic",
	"r": "Region",
	"i": "RegionID",
	"c": "RegionCode",
	"m": "RegionName",
	"N": "Nodes",
	"n": "Name",
	"h": "HostName",
	"t": "CertName",
	"4": "IPv4",
	"6": "IPv6",
	"s": "STUNPort",
	"d": "DERPPort",
	"x": "InsecureForTests",
}

// TestWireFieldNames verifies that every field of every wire type
// has a CBOR field name matching wireFieldNames, so that the same Go
// field name always gets the same short name (and no short name ever
// means two different things).
func TestWireFieldNames(t *testing.T) {
	longToShort := make(map[string]string)
	for short, long := range wireFieldNames {
		if prev, ok := longToShort[long]; ok {
			t.Fatalf("wireFieldNames maps both %q and %q to %q", prev, short, long)
		}
		longToShort[long] = short
	}

	seen := make(map[string]bool)
	for _, typ := range []reflect.Type{
		reflect.TypeFor[wireConnInfo](),
		reflect.TypeFor[wireRegion](),
		reflect.TypeFor[wireNode](),
	} {
		for i := range typ.NumField() {
			f := typ.Field(i)
			short, _, _ := strings.Cut(f.Tag.Get("cbor"), ",")
			if short == "" {
				t.Errorf("%v.%s: missing cbor field name", typ, f.Name)
				continue
			}
			if want, ok := longToShort[f.Name]; !ok {
				t.Errorf("%v.%s: field not in wireFieldNames", typ, f.Name)
			} else if short != want {
				t.Errorf("%v.%s: cbor field name %q; want %q", typ, f.Name, short, want)
			}
			seen[short] = true
		}
	}
	for short, long := range wireFieldNames {
		if !seen[short] {
			t.Errorf("wireFieldNames entry %q => %q matches no wire type field", short, long)
		}
	}
}

// TestWireRegionRoundTrip tests the mapping between the upstream
// tailcfg DERP types (as fetched from the control plane's DERP map)
// and tailcat's wire types: fields tailcat uses survive the round
// trip, unused fields are dropped, and STUN-only nodes disappear.
func TestWireRegionRoundTrip(t *testing.T) {
	in := &tailcfg.DERPRegion{
		RegionID:   10,
		RegionCode: "sea",
		RegionName: "Seattle",
		Latitude:   47.609722,   // dropped
		Longitude:  -122.333056, // dropped
		Avoid:      true,        // dropped
		Nodes: []*tailcfg.DERPNode{
			{
				Name:      "10b",
				RegionID:  10,
				HostName:  "derp10b.tailscale.com",
				CertName:  "cert.example.com", // differs from HostName; must survive
				IPv4:      "192.73.240.161",
				IPv6:      "2607:f740:f::a01",
				STUNPort:  3478,
				DERPPort:  8443,
				CanPort80: true, // dropped
			},
			{
				Name:     "10s",
				RegionID: 10,
				HostName: "stun-only.tailscale.com",
				STUNOnly: true, // whole node dropped
			},
			{
				Name:             "custom",
				HostName:         "my-derp.example.com",
				IPv6:             "none",
				STUNPort:         -1,
				InsecureForTests: true,
			},
		},
	}

	want := &tailcfg.DERPRegion{
		RegionID:   10,
		RegionCode: "sea",
		RegionName: "Seattle",
		Nodes: []*tailcfg.DERPNode{
			{
				Name:     "10b",
				RegionID: 10,
				HostName: "derp10b.tailscale.com",
				CertName: "cert.example.com",
				IPv4:     "192.73.240.161",
				IPv6:     "2607:f740:f::a01",
				STUNPort: 3478,
				DERPPort: 8443,
			},
			{
				Name:             "custom",
				HostName:         "my-derp.example.com",
				IPv6:             "none",
				STUNPort:         -1,
				InsecureForTests: true,
			},
		},
	}

	got := wireRegionOf(in).derpRegion()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round trip diff (-want +got):\n%s", diff)
	}
}
