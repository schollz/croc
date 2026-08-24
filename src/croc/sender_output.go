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
	instructions := fmt.Sprintf(`On the other computer, run:
  croc %s%s%s
`, flags, colorSecret(secret, colorEnabled), clipboardNotice)
	if webURL != "" {
		instructions += fmt.Sprintf(`
Or open:
  %s
`, webURL)
	}
	return instructions
}

func formatClipboardText(secret, flags string, extended bool) string {
	if !extended {
		return secret
	}
	return fmt.Sprintf("CROC_SECRET=%q croc %s", secret, strings.TrimSpace(flags))
}
