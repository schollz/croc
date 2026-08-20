package croc

import (
	"fmt"
	"strings"

	"github.com/schollz/croc/v11/src/termui"
)

const (
	secretColorPrefix = termui.Yellow
	colorReset        = termui.Reset
)

func colorSecret(secret string, enabled bool) string {
	if !enabled {
		return secret
	}
	return termui.Secret(secret, true)
}

func formatSendInstructions(secret, flags, webURL, clipboardNotice string, colorEnabled bool) string {
	if clipboardNotice != "" {
		clipboardNotice = " (" + clipboardNotice + ")"
	}
	return fmt.Sprintf(`On the other computer, run:
  croc %[2]s%[1]s%[4]s

Or open:
  %[3]s
`, colorSecret(secret, colorEnabled), flags, webURL, clipboardNotice)
}

func formatClipboardText(secret, flags string, extended bool) string {
	if !extended {
		return secret
	}
	return fmt.Sprintf("CROC_SECRET=%q croc %s", secret, strings.TrimSpace(flags))
}
