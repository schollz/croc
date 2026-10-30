package croc

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/schollz/croc/v11/src/bundle"
	"github.com/schollz/croc/v11/src/message"
)

func TestReceiveRenameReservesPortableManifestPaths(t *testing.T) {
	for _, reserved := range []string{"a (1).bin", "A (1).BIN"} {
		t.Run(reserved, func(t *testing.T) {
			c := bundleTestClient(t)
			t.Chdir(t.TempDir())
			c.Options.IsSender, c.Options.Rename = false, true
			c.FilesToTransfer[1].Name = reserved
			if err := os.WriteFile("a.bin", []byte("existing"), 0o600); err != nil {
				t.Fatal(err)
			}
			decision, err := c.preflightReceiveFile(0)
			if err != nil || !decision.fresh || c.FilesToTransfer[0].Name != "a (2).bin" {
				t.Fatalf("rename = %q, decision=%+v, error=%v", c.FilesToTransfer[0].Name, decision, err)
			}
			if _, _, err = validateReceiveMetadata(c.FilesToTransfer, nil); err != nil {
				t.Fatal(err)
			}
			// Revisiting a plan keeps the accepted destination.
			if _, err = c.preflightReceiveFile(0); err != nil || c.FilesToTransfer[0].Name != "a (2).bin" {
				t.Fatal(err)
			}
		})
	}
}

func TestBundleFailedDiscardRetainsOwnerAndBlocksAdvance(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions")
	}
	c := bundleTestClient(t)
	directory := t.TempDir()
	spool, err := os.CreateTemp(directory, "croc-bundle-test-")
	if err != nil {
		t.Fatal(err)
	}
	c.bundleState().spool = spool
	if err = os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(directory, 0o700); c.removeBundleSpool() })
	if err = c.bundleState().lifecycle.Request(1); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"offer", "ready"} {
		if err = c.bundleState().lifecycle.Advance(1, phase); err != nil {
			t.Fatal(err)
		}
	}
	c.bundles.peerHandshake = true
	c.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
	err = c.processBundleMessage(message.Message{Type: message.TypeBundleDone, Bytes: []byte(`{"id":1}`)}, new(transferAttemptState))
	var cleanup bundleCleanupError
	if !errors.As(err, &cleanup) || c.bundleState().spool != spool || c.bundleState().lifecycle.ActiveID != 1 {
		t.Fatalf("failed discard lost ownership or advanced: %v", err)
	}
	if _, err = os.Stat(spool.Name()); err != nil {
		t.Fatal(err)
	}
	if isTransferDisconnectError(errors.Join(transferDisconnectError{err: errors.New("disconnect")}, cleanup)) {
		t.Fatal("cleanup failure became reconnectable")
	}
	if _, err = c.prepareBundle(bundle.Request{}); err == nil {
		t.Fatal("replaced an unreleased payload")
	}
	if err = os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = c.removeBundleSpool(); err != nil || c.bundleState().spool != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(spool.Name()); !os.IsNotExist(err) {
		t.Fatal("retry did not remove spool", err)
	}
}

func TestBundleFailedStageCleanupRetainsOwner(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions")
	}
	c := bundleTestClient(t)
	directory := t.TempDir()
	t.Chdir(directory)
	root, err := c.receiveFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	stage, err := root.CreateTempDir(".", ".croc-bundle-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(stage, "member"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.bundleState().stageRoot, c.bundleState().stageDir = root, stage
	if err = os.Chmod(stage, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(stage, 0o700); c.removeBundleStage() })
	var cleanup bundleCleanupError
	if err = c.removeBundleStage(); !errors.As(err, &cleanup) || c.bundleState().stageDir == "" {
		t.Fatalf("staging ownership lost: %v", err)
	}
	if err = os.Chmod(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = c.removeBundleStage(); err != nil || c.bundleState().stageDir != "" {
		t.Fatal(err)
	}
}
