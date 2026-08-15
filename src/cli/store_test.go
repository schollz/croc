package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
	"github.com/schollz/croc/v11/src/storeclient"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/utils"
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

func TestStoredCallbacksUseRegularCrocPalette(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStyledStoredCallbacks(&output, false, true)
	callbacks.Progress(storeclient.Progress{
		FileName:   "LICENSE",
		TotalBytes: 50,
		TotalSize:  100,
	})
	got := output.String()
	if !strings.Contains(got, termui.Bold+"LICENSE"+termui.Reset) {
		t.Fatalf("stored progress filename is not bold: %q", got)
	}
	if !strings.Contains(got, termui.Cyan+"50.0%"+termui.Reset) {
		t.Fatalf("stored progress percentage is not cyan: %q", got)
	}

	callbacks.Status("Encrypted upload complete")
	got = output.String()
	if !strings.Contains(got, termui.Green+"Encrypted upload complete"+termui.Reset) {
		t.Fatalf("stored completion is not green: %q", got)
	}
}

func TestStoredCallbacksMeasureStyledUnicodeByDisplayWidth(t *testing.T) {
	var output bytes.Buffer
	callbacks := newStyledStoredCallbacks(&output, false, true)
	callbacks.Status("Uploading 🎉")
	callbacks.Status("Done")

	wantClear := "\r" + strings.Repeat(" ", uniseg.StringWidth("Uploading 🎉")) + "\r"
	if got := output.String(); !strings.Contains(got, wantClear) {
		t.Fatalf("styled Unicode status did not clear by display width: %q", got)
	}
}

func TestFormatStoredSendInstructionsUsesRegularCrocPalette(t *testing.T) {
	plain := formatStoredSendInstructions(
		"tomorrow", "https://example.com/#secret", "croc-store-v1.token", "transfer-id", "one verified download", false,
	)
	colored := formatStoredSendInstructions(
		"tomorrow", "https://example.com/#secret", "croc-store-v1.token", "transfer-id", "one verified download", true,
	)
	if termui.Plain(colored) != plain {
		t.Fatalf("colored stored instructions changed text:\n%s", colored)
	}
	for _, secret := range []string{"https://example.com/#secret", "croc-store-v1.token", "transfer-id"} {
		if !strings.Contains(colored, termui.Yellow+secret+termui.Reset) {
			t.Fatalf("stored instructions do not highlight %q: %q", secret, colored)
		}
	}
	if !strings.Contains(colored, termui.Green+"Stored transfer is encrypted") {
		t.Fatalf("stored ready message is not green: %q", colored)
	}
}
