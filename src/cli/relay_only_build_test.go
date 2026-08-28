//go:build croc_no_tailcat

package cli

import (
	"testing"

	"github.com/schollz/croc/v11/src/croc"
)

func TestRelayOnlySendTransportNormalizesRememberedDERP(t *testing.T) {
	options := croc.Options{Transport: croc.TransportDERP}
	downgraded, err := resolveSendTransport(&options)
	if err != nil {
		t.Fatal(err)
	}
	if options.Transport != croc.TransportRelay || !downgraded {
		t.Fatalf("resolved transport = (%q, %t); want (relay, true)", options.Transport, downgraded)
	}
}
