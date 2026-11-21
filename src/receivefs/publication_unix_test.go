//go:build !windows

package receivefs

import (
	"os"
	"syscall"
	"testing"
)

// Android shared storage (for example Termux writing to /sdcard) refuses
// link(2) with EACCES even though the probe could create both directories
// and the file. The receiver should fall back to individual transfers.
func TestPublicationCapabilityTreatsLinkDenialAsUnsupported(t *testing.T) {
	root, err := OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, cause := range []syscall.Errno{syscall.EPERM, syscall.EACCES} {
		t.Run(cause.Error(), func(t *testing.T) {
			supported, err := root.probePublishNew("nested", func(string, string) error {
				return &os.LinkError{Op: "link", Err: cause}
			})
			if supported || err != nil {
				t.Fatalf("probe: %v, %v; want false, nil", supported, err)
			}
		})
	}
}
