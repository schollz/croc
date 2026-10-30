package croc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/schollz/croc/v11/src/termui"
)

func TestBundleProgressSharesOneLineAcrossMixedUnits(t *testing.T) {
	for _, colorEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "color"}[colorEnabled], func(t *testing.T) {
			var output strings.Builder
			c := &Client{Options: Options{HashAlgorithm: defaultHashAlgorithm}}
			c.FilesToTransfer = []FileInfo{
				{Name: "first.bin", Size: 5}, {Name: "second.bin", Size: 7},
				{Name: "empty.bin"}, {Name: "individual.bin", Size: 20},
				{Name: "long-filename-🎉.bin", Size: 8}, {Name: "last.bin", Size: 6},
			}
			c.TotalNumberOfContents = len(c.FilesToTransfer)
			c.bundles.peerHandshake, c.bundles.manifest = true, true
			c.progressDisplay = newTransferProgressDisplay(c.FilesToTransfer, 0, &output, colorEnabled)
			display := c.progressDisplay
			bar := display.bar

			// A multiplexed bundle can deliver its members out of order.
			progress := c.newBundleProgress([]int{0, 1}, []int64{0, 5})
			progress(5, 7)
			progress(0, 5)
			c.fmtPrintUpdate()
			c.fmtPrintUpdate()

			c.setProgressBar(c.newProgressBar(1, "empty.bin", 0))
			c.finishProgress()
			c.setProgressBar(c.newProgressBar(20, "individual.bin", 0))
			c.addProgress(9)
			c.finishProgress()
			c.finishProgress()

			progress = c.newBundleProgress([]int{4, 5}, []int64{0, 8})
			progress(0, 10) // A chunk can cross a member boundary.
			progress(10, 4)
			c.fmtPrintUpdate()
			c.fmtPrintUpdate()
			if strings.Contains(output.String(), "\n") {
				t.Fatalf("file completions emitted extra lines: %q", output.String())
			}
			if display.bar != bar || c.progressDisplay != display {
				t.Fatal("mixed units replaced the shared progress bar")
			}
			if c.finishedNum != len(c.FilesToTransfer) {
				t.Fatalf("logical completions = %d; want %d", c.finishedNum, len(c.FilesToTransfer))
			}
			if got := bar.State().CurrentBytes; got != 47 {
				t.Fatalf("aggregate progress = %v bytes; want 47", got)
			}
			display.close(true)
			display.close(true)
			got := termui.Plain(output.String())
			if strings.Count(got, "\n") != 1 || !strings.Contains(got, "6/6 last.bin") || !strings.Contains(got, "100%") {
				t.Fatalf("final progress is not one completed line: %q", got)
			}
			if !utf8.ValidString(got) {
				t.Fatal("progress split a filename's UTF-8 encoding")
			}
		})
	}
}

func TestBundleProgressDoesNotFinishInterruptedTransfer(t *testing.T) {
	var output strings.Builder
	display := newTransferProgressDisplay([]FileInfo{{Name: "first.bin", Size: 100}, {Name: "last.bin", Size: 100}}, 0, &output, false)
	display.begin(100, "first.bin", 1)
	display.advance("", 0, 20)
	display.close(false)
	if display.bar.State().CurrentBytes != 20 || strings.Contains(output.String(), "100%") {
		t.Fatalf("interrupted transfer was displayed as complete: %q", output.String())
	}
	if strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("interrupted transfer left an unterminated line: %q", output.String())
	}
}
