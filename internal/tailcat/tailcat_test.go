// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailcat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go4.org/mem"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
	"tailscale.com/wgengine/filter"
)

func mkLogger(t testing.TB, name string) logger.Logf {
	return func(format string, args ...any) {
		t.Helper()
		if t.Failed() {
			return
		}
		t.Logf("        ["+name+"] "+format, args...)
	}
}

func TestTailcat(t *testing.T) {
	t.Parallel()

	dm := integration.RunDERPAndSTUN(t, mkLogger(t, "derpstun"), "127.0.0.1")
	t.Logf("DERPMap: %v", logger.AsJSON(dm))

	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("no region 1 in derpmap")
	}
	priv := key.NewNode()

	s := &Server{Key: priv, Logf: mkLogger(t, "server"), Region: reg}
	t.Cleanup(func() { s.Close() })

	s.OnTCP = func(port uint16) (handler func(net.Conn)) {
		t.Logf("test: OnTCP(port %v) ...", port)
		if port != 80 {
			return nil
		}
		return func(c net.Conn) {
			io.WriteString(c, "Hello from port 80\n")
			c.Close()
		}
	}
	s.OnTCPForward = func(dst netip.AddrPort) (handler func(net.Conn)) {
		t.Logf("test: OnTCPForward(%v) ...", dst)
		return func(c net.Conn) {
			io.WriteString(c, "Hello from relay\n")
			c.Close()
		}
	}
	// Start with a non-matching allowlist entry so the first ping can verify
	// that disallowed clients get no acknowledgement.
	s.AddAllowedClient(key.NewNode().Public())

	if err := s.Start(); err != nil {
		t.Fatalf("server Start: %v", err)
	}
	t.Logf("server: %v", s.ConnBlob())

	c := &Client{Server: s.ConnBlob(), Logf: mkLogger(t, "client")}
	t.Cleanup(func() { c.Close() })

	t.Logf("Client is %v", c.PublicKey())

	WaitForDERPForTest(t, s, c)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	if _, err := c.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ping from disallowed client = %v; want context deadline exceeded", err)
	}
	cancel()
	s.AddAllowedClient(c.PublicKey())

	pi := PingForTest(t, s, c)
	t.Logf("got ping: %+v", pi)

	// No sleep here: a successful Ping means the server has fully
	// added us as a peer and we may dial immediately.

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := c.DialTCPPort(ctx, 80)
	if err != nil {
		t.Fatalf("UserDial = %v, %v", conn, err)
	}
	all, err := io.ReadAll(conn)
	t.Logf("Got: %q, %v", all, err)

	// And dialing arbitrary IPs...
	conn, err = c.DialTCP(ctx, netip.MustParseAddrPort("192.0.2.1:123"))
	if err != nil {
		t.Fatalf("DialTCP = %v, %v", conn, err)
	}

}

// TestHalfClose tests that a client's write shutdown (CloseWrite)
// propagates through the server's TCP proxying as a half-close
// rather than tearing down the whole connection: the backend must
// still be able to send its response after seeing the client's EOF,
// netcat style.
func TestHalfClose(t *testing.T) {
	t.Parallel()

	dm := integration.RunDERPAndSTUN(t, mkLogger(t, "derpstun"), "127.0.0.1")
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("no region 1 in derpmap")
	}

	// The backend reads until EOF and only then writes its response.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				got, err := io.ReadAll(c)
				if err != nil {
					t.Logf("backend read: %v", err)
					return
				}
				fmt.Fprintf(c, "read %d bytes", len(got))
			}()
		}
	}()

	s := &Server{Logf: mkLogger(t, "server"), Region: reg}
	t.Cleanup(func() { s.Close() })
	s.OnTCP = func(port uint16) (handler func(net.Conn)) {
		return func(c net.Conn) {
			backend, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Logf("backend dial: %v", err)
				c.Close()
				return
			}
			ProxyConns(c, backend)
		}
	}
	s.ServedTCPPorts = []filter.PortRange{{First: 80, Last: 80}}
	if err := s.Start(); err != nil {
		t.Fatalf("server Start: %v", err)
	}

	c := &Client{Server: s.ConnBlob(), Logf: mkLogger(t, "client")}
	t.Cleanup(func() { c.Close() })
	PingForTest(t, s, c)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := c.DialTCPPort(ctx, 80)
	if err != nil {
		t.Fatalf("DialTCPPort: %v", err)
	}
	defer conn.Close()

	const req = "hello, backend"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write: %v", err)
	}
	cw, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatalf("conn type %T doesn't support CloseWrite", conn)
	}
	if err := cw.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("reading response after half-close: %v", err)
	}
	if want := fmt.Sprintf("read %d bytes", len(req)); string(resp) != want {
		t.Fatalf("response = %q; want %q", resp, want)
	}

	// The packet filter (ServedTCPPorts) must drop SYNs to unserved
	// ports before they reach OnTCP, whose handler above would accept
	// any port. A filter drop is silent, so the dial must ride out
	// the context deadline rather than fail fast with a RST.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	c2, err := c.DialTCPPort(ctx2, 81)
	if err == nil {
		c2.Close()
		t.Fatal("dial to filtered port 81 unexpectedly succeeded")
	}
	if ctx2.Err() == nil {
		t.Fatalf("dial to filtered port 81 failed fast (%v); want silent drop until context deadline", err)
	}
}

func TestConnBlob(t *testing.T) {
	akey := func(a [32]byte) NodePublic {
		return NodePublic{key.NodePublicFromRaw32(mem.B(a[:]))}
	}
	tests := []struct {
		name string
		ci   ConnInfo
		want ConnBlob  // if non-empty, check exact encoding
		back *ConnInfo // if non-nil, round-tripped form we want
	}{
		{
			name: "just_key",
			ci: ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
			},
			want: "tcoWFwWCAAAQIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHw",
		},
		{
			name: "key_with_full_custom_region",
			ci: ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
				Region: []*tailcfg.DERPRegion{
					{
						Nodes: []*tailcfg.DERPNode{
							{
								Name:     "1a",
								IPv4:     "400.400.400.400",
								HostName: "my-derp.custom.example",
							},
							{
								Name:     "1b",
								IPv4:     "400.400.400.400",
								HostName: "my-derp2.custom.example",
							},
						},
					},
				},
			},
			back: &ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
				Region: []*tailcfg.DERPRegion{
					{
						RegionID:   1,
						RegionCode: "1",
						Nodes: []*tailcfg.DERPNode{
							{
								RegionID: 1,
								Name:     "my-derp.custom.example",
								IPv4:     "400.400.400.400",
								HostName: "my-derp.custom.example",
							},
							{
								RegionID: 1,
								Name:     "my-derp2.custom.example",
								IPv4:     "400.400.400.400",
								HostName: "my-derp2.custom.example",
							},
						},
					},
				},
			},
		},

		{
			name: "remove_implicit_fields_on_marshal",
			ci: ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
				Region: []*tailcfg.DERPRegion{
					{
						RegionID:   123,
						RegionName: "Seattle",
						Nodes: []*tailcfg.DERPNode{
							{
								RegionID: 123,
								Name:     "1a",
								HostName: "tc1a.ipn.dev",
							},
							{
								RegionID: 123,
								Name:     "1b",
								HostName: "derp1b.tailscale.com",
							},
						},
					},
				},
			},
			back: &ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
				Region: []*tailcfg.DERPRegion{
					{
						RegionID:   1,
						RegionCode: "1",
						Nodes: []*tailcfg.DERPNode{
							{
								RegionID: 1,
								Name:     "tc1a.ipn.dev",
								HostName: "tc1a.ipn.dev",
							},
							{
								RegionID: 1,
								Name:     "derp1b.tailscale.com",
								HostName: "derp1b.tailscale.com",
							},
						},
					},
				},
			},
		},

		{
			name: "region_id",
			ci: ConnInfo{
				ServerPublic: akey([32]byte{1: 1, 2: 2, 31: 31}),
				RegionID:     10,
			},
			want: "tcomFwWCAAAQIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAH2FpCg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ci.ConnBlob()
			t.Logf("length: %v (%v)", len(got), got)
			if tt.want != "" && got != tt.want {
				t.Fatalf("ConnInfo.ConnBlob marshal wrong.\n got: %s\nwant: %s\n", got, tt.want)
			}

			gotCI, err := ParseConnBlob(got)
			if err != nil {
				t.Fatalf("ParseConnBlob: %v", err)
			}
			want := tt.ci
			if tt.back != nil {
				want = *tt.back
			}
			if diff := cmp.Diff(want, gotCI); diff != "" {
				t.Errorf("ParseConnBlob result back diff:\n%s", diff)
			}
		})
	}
}

// TestFetchDERPMapMemoryCache verifies the default in-memory DERP map
// cache: a second fetch of the same URL within the freshness window
// makes no network request.
func TestFetchDERPMapMemoryCache(t *testing.T) {
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		fmt.Fprint(w, `{"Regions":{"1":{"RegionID":1}}}`)
	}))
	defer srv.Close()

	for range 2 {
		dm, err := FetchDERPMap(context.Background(), DERPMapURL(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if len(dm.Regions) != 1 {
			t.Fatalf("got %d regions; want 1", len(dm.Regions))
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("fetches = %d; want 1", n)
	}
}
