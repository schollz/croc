package derptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shayne/derphole/pkg/derpbind"
	"github.com/shayne/derphole/pkg/token"
	"tailscale.com/derp/derpserver"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

type transportAcceptResult struct {
	conn io.ReadWriteCloser
	err  error
}

func encodeTestToken(t *testing.T, expires time.Time, capabilities uint32, route derpbind.Route) string {
	t.Helper()
	encoded, err := token.Encode(token.Token{
		ExpiresUnix:     expires.Unix(),
		BootstrapRegion: 1,
		Capabilities:    capabilities,
		DERPRoute:       route,
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	return encoded
}

func TestValidateToken(t *testing.T) {
	now := time.Now()
	valid := encodeTestToken(t, now.Add(time.Minute), token.CapabilityAttach, derpbind.Route{})
	if err := ValidateToken(valid, now); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}

	tests := []struct {
		name  string
		value string
		want  error
	}{
		{name: "empty", value: "", want: ErrTokenSize},
		{name: "oversized", value: strings.Repeat("x", MaxTokenSize+1), want: ErrTokenSize},
		{name: "expired", value: encodeTestToken(t, now.Add(-time.Minute), token.CapabilityAttach, derpbind.Route{}), want: token.ErrExpired},
		{name: "wrong capability", value: encodeTestToken(t, now.Add(time.Minute), token.CapabilityShare, derpbind.Route{}), want: ErrCapability},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateToken(test.value, now); !errors.Is(err, test.want) {
				t.Fatalf("ValidateToken() error = %v, want %v", err, test.want)
			}
		})
	}

	route, err := derpbind.NewCustomRoute("derp.example", 443, 3478)
	if err != nil {
		t.Fatal(err)
	}
	custom := encodeTestToken(t, now.Add(time.Minute), token.CapabilityAttach, route)
	if err = ValidateToken(custom, now); !errors.Is(err, ErrCustomRoute) {
		t.Fatalf("custom route error = %v, want %v", err, ErrCustomRoute)
	}
}

func TestValidatePublicRouteRejectsEnvironmentOverride(t *testing.T) {
	t.Setenv(derpbind.CustomDERPServerEnv, "https://derp.example")
	if err := ValidatePublicRoute(); !errors.Is(err, ErrCustomRoute) {
		t.Fatalf("ValidatePublicRoute() error = %v, want %v", err, ErrCustomRoute)
	}
}

func TestPublicAttachRoundTripHermetic(t *testing.T) {
	if !Available() {
		t.Skip("DERP transport is unavailable on this platform")
	}
	derpServer := derpserver.New(key.NewNode(), t.Logf)
	t.Cleanup(func() { _ = derpServer.Close() })
	derpHTTP := httptest.NewServer(derpserver.Handler(derpServer))
	t.Cleanup(derpHTTP.Close)

	derpMap := &tailcfg.DERPMap{Regions: map[int]*tailcfg.DERPRegion{
		1: {
			RegionID:   1,
			RegionCode: "test",
			RegionName: "croc test",
			Nodes: []*tailcfg.DERPNode{{
				Name:     "croc-test-1",
				RegionID: 1,
				HostName: "127.0.0.1",
				IPv4:     "127.0.0.1",
				STUNPort: -1,
				DERPPort: 0,
			}},
		},
	}}
	mapHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(derpMap)
	}))
	t.Cleanup(mapHTTP.Close)
	t.Setenv("DERPHOLE_TEST_DERP_MAP_URL", mapHTTP.URL)
	t.Setenv("DERPHOLE_TEST_DERP_SERVER_URL", derpHTTP.URL+"/derp")
	t.Setenv(derpbind.CustomDERPServerEnv, "")
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var statusMu sync.Mutex
	var statuses []string
	events := func(status string) {
		statusMu.Lock()
		statuses = append(statuses, status)
		statusMu.Unlock()
	}

	listener, err := Listen(ctx, events)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	if err = ValidateToken(listener.Token(), time.Now()); err != nil {
		t.Fatalf("listener token invalid: %v", err)
	}

	accepted := make(chan transportAcceptResult, 1)
	go func() {
		conn, acceptErr := listener.Accept(ctx)
		accepted <- transportAcceptResult{conn: conn, err: acceptErr}
	}()
	dialConn, err := Dial(ctx, listener.Token(), events)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer dialConn.Close()
	result := <-accepted
	if result.err != nil {
		t.Fatalf("Accept() error = %v", result.err)
	}
	defer result.conn.Close()

	wantForward := []byte("sender to receiver")
	go func() { _, _ = dialConn.Write(wantForward) }()
	gotForward := make([]byte, len(wantForward))
	if _, err = io.ReadFull(result.conn, gotForward); err != nil || !bytes.Equal(gotForward, wantForward) {
		t.Fatalf("forward traffic = %q, %v", gotForward, err)
	}
	wantReverse := []byte("receiver to sender")
	go func() { _, _ = result.conn.Write(wantReverse) }()
	gotReverse := make([]byte, len(wantReverse))
	if _, err = io.ReadFull(dialConn, gotReverse); err != nil || !bytes.Equal(gotReverse, wantReverse) {
		t.Fatalf("reverse traffic = %q, %v", gotReverse, err)
	}

	statusMu.Lock()
	joinedStatuses := strings.Join(statuses, ",")
	statusMu.Unlock()
	if !strings.Contains(joinedStatuses, "connected-relay") && !strings.Contains(joinedStatuses, "connected-direct") {
		t.Fatalf("transport statuses = %q, want a connected path", joinedStatuses)
	}
}

func TestPublicAttachRoundTripLive(t *testing.T) {
	if !Available() {
		t.Skip("DERP transport is unavailable on this platform")
	}
	if os.Getenv("CROC_TEST_PUBLIC_DERP") != "1" {
		t.Skip("set CROC_TEST_PUBLIC_DERP=1 to run against the public Tailscale DERP network")
	}
	t.Setenv("DERPHOLE_TEST_DERP_MAP_URL", "")
	t.Setenv("DERPHOLE_TEST_DERP_SERVER_URL", "")
	t.Setenv(derpbind.CustomDERPServerEnv, "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	listener, err := Listen(ctx, nil)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	accepted := make(chan transportAcceptResult, 1)
	go func() {
		conn, acceptErr := listener.Accept(ctx)
		accepted <- transportAcceptResult{conn: conn, err: acceptErr}
	}()
	dialConn, err := Dial(ctx, listener.Token(), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer dialConn.Close()
	result := <-accepted
	if result.err != nil {
		t.Fatalf("Accept() error = %v", result.err)
	}
	defer result.conn.Close()

	want := []byte("public DERP smoke test")
	go func() { _, _ = dialConn.Write(want) }()
	got := make([]byte, len(want))
	if _, err = io.ReadFull(result.conn, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("traffic = %q, %v", got, err)
	}
}
