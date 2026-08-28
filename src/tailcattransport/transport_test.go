//go:build !dragonfly && !netbsd

package tailcattransport

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"tailscale.com/tailcfg"
)

func TestConfigValidation(t *testing.T) {
	for _, count := range []int{MinStreamCount, 4, MaxStreamCount} {
		if err := (Config{StreamCount: count}).Validate(); err != nil {
			t.Fatalf("stream count %d: %v", count, err)
		}
	}
	for _, count := range []int{0, -1, MaxStreamCount + 1} {
		if err := (Config{StreamCount: count}).Validate(); err == nil {
			t.Fatalf("stream count %d unexpectedly valid", count)
		}
	}
}

func TestDerivedRoleIdentitiesAreStableAndSeparate(t *testing.T) {
	keyMaterial := []byte("confirmed PAKE encryption key for Tailcat test")
	sender, receiver, err := DerivePublicKeys(keyMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if sender == receiver {
		t.Fatal("sender and receiver identities are equal")
	}
	senderAgain, receiverAgain, err := DerivePublicKeys(keyMaterial)
	if err != nil {
		t.Fatal(err)
	}
	if sender != senderAgain || receiver != receiverAgain {
		t.Fatal("derived identities are not deterministic")
	}
	otherSender, _, err := DerivePublicKeys([]byte("different confirmed PAKE key"))
	if err != nil {
		t.Fatal(err)
	}
	if sender == otherSender {
		t.Fatal("different PAKE keys produced the same sender identity")
	}
}

func TestValidateOfferRequiresAuthenticatedEmbeddedRelay(t *testing.T) {
	sessionKey := []byte("offer validation PAKE key")
	sender, _, err := DerivePublicKeys(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	region := &tailcfg.DERPRegion{
		RegionID:   91,
		RegionCode: "test",
		RegionName: "test",
		Nodes: []*tailcfg.DERPNode{{
			Name:     "test-1",
			RegionID: 91,
			HostName: "127.0.0.1",
			DERPPort: 443,
		}},
	}
	valid := string((&tailcat.ConnInfo{
		ServerPublic: tailcat.NodePublic{NodePublic: sender},
		Region:       []*tailcfg.DERPRegion{region},
	}).ConnBlob())
	if err := ValidateOffer(valid, sessionKey); err != nil {
		t.Fatalf("valid offer rejected: %v", err)
	}

	tests := []struct {
		name  string
		offer string
		key   []byte
	}{
		{name: "empty", offer: "", key: sessionKey},
		{name: "oversized", offer: strings.Repeat("x", MaxOfferSize+1), key: sessionKey},
		{name: "malformed", offer: "tc-not-cbor", key: sessionKey},
		{name: "wrong identity", offer: valid, key: []byte("other session")},
		{name: "non embedded", offer: string((&tailcat.ConnInfo{ServerPublic: tailcat.NodePublic{NodePublic: sender}, RegionID: 91}).ConnBlob()), key: sessionKey},
		{name: "missing relay", offer: string((&tailcat.ConnInfo{ServerPublic: tailcat.NodePublic{NodePublic: sender}, Region: []*tailcfg.DERPRegion{{RegionID: 91}}}).ConnBlob()), key: sessionKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateOffer(tt.offer, tt.key); !errors.Is(err, ErrInvalidOffer) {
				t.Fatalf("error = %v, want ErrInvalidOffer", err)
			}
		})
	}
}

func TestBundleCountsTrafficAndClosesIdempotently(t *testing.T) {
	left, right := net.Pipe()
	ownerClosed := 0
	bundle := newBundle([]net.Conn{left}, time.Now(), &sharedOwner{close: func() error {
		ownerClosed++
		return nil
	}}, nil, newPathTracker(nil))
	conn := bundle.Connections()[0]
	written := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("hello"))
		written <- err
	}()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(right, buf); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = right.Write([]byte("world!")) }()
	if _, err := io.ReadFull(conn, make([]byte, 6)); err != nil {
		t.Fatal(err)
	}
	stats := bundle.Stats()
	if stats.StreamCount != 1 || stats.BytesSent != 5 || stats.BytesReceived != 6 {
		t.Fatalf("stats = %+v", stats)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	if ownerClosed != 1 {
		t.Fatalf("owner closed %d times", ownerClosed)
	}
	_ = right.Close()
}

func TestPathTrackerEmitsEachTransitionOnce(t *testing.T) {
	var events []string
	tracker := newPathTracker(func(event string) { events = append(events, event) })
	tracker.set("connecting")
	tracker.set("connecting")
	tracker.set("derp")
	tracker.set("derp")
	tracker.set("direct")
	if got, want := strings.Join(events, ","), "path=connecting,path=derp,path=direct"; got != want {
		t.Fatalf("path events = %q, want %q", got, want)
	}
	if got := tracker.get(); got != "direct" {
		t.Fatalf("current path = %q, want direct", got)
	}
}

func TestListenerRejectsUnexpectedAndDuplicatePorts(t *testing.T) {
	var events []string
	listener := &Listener{
		config:   Config{StreamCount: 2},
		accepted: make([]bool, 2),
		channels: []chan net.Conn{make(chan net.Conn, 1), make(chan net.Conn, 1)},
		events:   func(event string) { events = append(events, event) },
	}
	if handler := listener.handlerForPort(FirstTCPPort - 1); handler != nil {
		t.Fatal("unexpected virtual port was accepted")
	}
	if handler := listener.handlerForPort(FirstTCPPort); handler == nil {
		t.Fatal("first expected virtual port was rejected")
	}
	if handler := listener.handlerForPort(FirstTCPPort); handler != nil {
		t.Fatal("duplicate virtual port was accepted")
	}
	if handler := listener.handlerForPort(FirstTCPPort + 1); handler == nil {
		t.Fatal("second expected virtual port was rejected")
	}
	if len(events) != 2 || !strings.Contains(events[0], "unexpected") || !strings.Contains(events[1], "duplicate") {
		t.Fatalf("rejection events = %v", events)
	}
}

func TestExpectedCloseRecognition(t *testing.T) {
	for _, err := range []error{nil, io.EOF, io.ErrClosedPipe, net.ErrClosed, context.Canceled} {
		if !IsExpectedClose(err) {
			t.Fatalf("%v was not recognized as expected", err)
		}
	}
	if IsExpectedClose(errors.New("checksum mismatch")) {
		t.Fatal("unexpected error recognized as a clean close")
	}
}
