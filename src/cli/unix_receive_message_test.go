package cli

import (
	"strings"
	"testing"

	"github.com/schollz/croc/v11/src/termui"
)

func TestFormatUnixReceiveCodeMessagePlain(t *testing.T) {
	want := `For security, croc does not accept receive codes on the UNIX
command line because they can appear in the process list.

Receive more securely with the code you entered:

  CROC_SECRET='film-alibi-jet' croc

Or enter it interactively:

  croc
  Enter receive code: film-alibi-jet

To allow command-line codes again, enable classic mode:

  croc --classic

`

	got := formatUnixReceiveCodeMessage("film-alibi-jet", false)
	if got != want {
		t.Fatalf("message differs:\nwant: %q\n got: %q", want, got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("plain message contains an ANSI escape: %q", got)
	}
}

func TestFormatUnixReceiveCodeMessageColor(t *testing.T) {
	got := formatUnixReceiveCodeMessage("film-alibi-jet", true)
	if termui.Plain(got) != formatUnixReceiveCodeMessage("film-alibi-jet", false) {
		t.Fatal("colored message differs from plain message after stripping styling")
	}
	if !strings.Contains(got, termui.Yellow+"film-alibi-jet"+termui.Reset) {
		t.Fatal("receive code is not highlighted")
	}
	if !strings.Contains(got, termui.Cyan+"croc --classic"+termui.Reset) {
		t.Fatal("classic command is not highlighted")
	}
}

func TestFormatUnixReceiveCodeMessageQuotesForShell(t *testing.T) {
	got := formatUnixReceiveCodeMessage("don't-panic", false)
	if !strings.Contains(got, `CROC_SECRET='don'\''t-panic' croc`) {
		t.Fatalf("environment command is not safely quoted: %q", got)
	}
}
