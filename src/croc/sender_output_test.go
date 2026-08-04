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

	want := `Code is: acid-pink-fostered-succeeding

On the other computer run:
(For Windows)
    croc --relay relay.example:9009 acid-pink-fostered-succeeding
(For Linux/macOS)
    CROC_SECRET="acid-pink-fostered-succeeding" croc --relay relay.example:9009 

Or receive in a browser:
    https://getcroc.com/?code=acid-pink-fostered-succeeding
`

	got := formatSendInstructions(secret, flags, webURL, false)
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
	want := "Code is: " + styledSecret + `

On the other computer run:
(For Windows)
    croc --relay relay.example:9009 ` + styledSecret + `
(For Linux/macOS)
    CROC_SECRET="` + styledSecret + `" croc --relay relay.example:9009 

Or receive in a browser:
    https://getcroc.com/?code=acid-pink-fostered-succeeding
`

	got := formatSendInstructions(secret, flags, webURL, true)
	if got != want {
		t.Fatalf("colored instructions differ:\nwant: %q\n got: %q", want, got)
	}
	if count := strings.Count(got, secretColorPrefix); count != 3 {
		t.Fatalf("color prefix count = %d; want 3", count)
	}
	urlSection := got[strings.Index(got, "Or receive in a browser:"):]
	if strings.Contains(urlSection, "\x1b") {
		t.Fatal("browser URL contains an ANSI escape")
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
