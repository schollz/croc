package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
	"github.com/schollz/croc/v10/src/storeclient"
	"github.com/schollz/croc/v10/src/utils"
)

func TestStoredCallbacksClearPreviousLine(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, false)
	progress := storeclient.Progress{
		FileName:   "LICENSE",
		TotalBytes: 50,
		TotalSize:  100,
	}
	callbacks.Progress(progress)
	callbacks.Status("Encrypted upload complete")

	progressLine := "LICENSE — 50.0% (" +
		utils.ByteCountDecimal(progress.TotalBytes) + " / " +
		utils.ByteCountDecimal(progress.TotalSize) + ")"
	want := "\r" + progressLine +
		"\r" + strings.Repeat(" ", uniseg.StringWidth(progressLine)) +
		"\rEncrypted upload complete"
	if got := output.String(); got != want {
		t.Fatalf("stored progress output = %q; want %q", got, want)
	}
}

func TestStoredCallbacksClearUnicodeLineByDisplayWidth(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, false)
	callbacks.Status("🎉")
	callbacks.Status("Done")

	want := "\r🎉\r  \rDone"
	if got := output.String(); got != want {
		t.Fatalf("stored Unicode status output = %q; want %q", got, want)
	}
}

func TestStoredCallbacksQuiet(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStoredCallbacks(&output, true)
	callbacks.Status("Preparing")
	callbacks.Progress(storeclient.Progress{
		FileName:   "LICENSE",
		TotalBytes: 50,
		TotalSize:  100,
	})
	if output.Len() != 0 {
		t.Fatalf("quiet stored progress output = %q; want no output", output.String())
	}
}
