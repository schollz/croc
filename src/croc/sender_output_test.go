package croc

import (
	"strings"
	"testing"
)

func TestFormatSendInstructionsPlain(t *testing.T) {
	const (
		secret = "acid-pink-fostered-succeeding"
		flags  = "--relay relay.example:9009 "
		webURL = "https://getcroc.com/?code=acid-pink-fostered-succeeding"
	)

	want := `On the other computer, run:
  croc --relay relay.example:9009 acid-pink-fostered-succeeding

Or open:
  https://getcroc.com/?code=acid-pink-fostered-succeeding
`

	got := formatSendInstructions(secret, flags, webURL, "", false)
	if got != want {
		t.Fatalf("plain instructions differ:\nwant: %q\n got: %q", want, got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("plain instructions contain an ANSI escape: %q", got)
	}
}

func TestFormatSendInstructionsColor(t *testing.T) {
	const (
		secret = "acid-pink-fostered-succeeding"
		flags  = "--relay relay.example:9009 "
		webURL = "https://getcroc.com/?code=acid-pink-fostered-succeeding"
	)

	styledSecret := secretColorPrefix + secret + colorReset
	want := `On the other computer, run:
  croc --relay relay.example:9009 ` + styledSecret + `

Or open:
  https://getcroc.com/?code=acid-pink-fostered-succeeding
`

	got := formatSendInstructions(secret, flags, webURL, "", true)
	if got != want {
		t.Fatalf("colored instructions differ:\nwant: %q\n got: %q", want, got)
	}
	if count := strings.Count(got, secretColorPrefix); count != 1 {
		t.Fatalf("color prefix count = %d; want 1", count)
	}
	urlSection := got[strings.Index(got, "Or open:"):]
	if strings.Contains(urlSection, "\x1b") {
		t.Fatal("browser URL contains an ANSI escape")
	}
}

func TestFormatSendInstructionsClipboardNotice(t *testing.T) {
	const secret = "film-alibi-jet"
	want := `On the other computer, run:
  croc film-alibi-jet (code copied to clipboard)

Or open:
  https://getcroc.com/?code=film-alibi-jet
`

	got := formatSendInstructions(
		secret,
		"",
		"https://getcroc.com/?code=film-alibi-jet",
		"code copied to clipboard",
		false,
	)
	if got != want {
		t.Fatalf("instructions differ:\nwant: %q\n got: %q", want, got)
	}
}

func TestFormatSendInstructionsWithoutBrowserURL(t *testing.T) {
	const secret = "film-alibi-jet"
	want := `On the other computer, run:
  croc film-alibi-jet
`

	got := formatSendInstructions(secret, "", "", "", false)
	if got != want {
		t.Fatalf("instructions differ:\nwant: %q\n got: %q", want, got)
	}
	if strings.Contains(got, "Or open:") || strings.Contains(got, "getcroc.com") {
		t.Fatalf("DERP instructions contain browser receive output: %q", got)
	}
}

func TestFormatClipboardTextHasNoStyling(t *testing.T) {
	const secret = "acid-pink-fostered-succeeding"

	tests := []struct {
		name     string
		extended bool
		want     string
	}{
		{name: "code only", want: secret},
		{
			name:     "extended command",
			extended: true,
			want:     `CROC_SECRET="acid-pink-fostered-succeeding" croc --relay relay.example:9009`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := formatClipboardText(secret, "--relay relay.example:9009 ", test.extended)
			if got != test.want {
				t.Fatalf("clipboard text = %q; want %q", got, test.want)
			}
			if strings.Contains(got, "\x1b") {
				t.Fatalf("clipboard text contains an ANSI escape: %q", got)
			}
		})
	}
}

func TestFormatExtendedClipboardOmitsSenderTransport(t *testing.T) {
	got := formatClipboardText("acid-pink-fostered-succeeding", "--relay relay.example:9009 ", true)
	want := `CROC_SECRET="acid-pink-fostered-succeeding" croc --relay relay.example:9009`
	if got != want {
		t.Fatalf("clipboard text = %q; want %q", got, want)
	}
}
