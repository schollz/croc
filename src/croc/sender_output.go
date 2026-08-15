package croc

import (
	"fmt"
	"strconv"
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

func colorQuotedSecret(secret string, enabled bool) string {
	quoted := strconv.Quote(secret)
	if !enabled {
		return quoted
	}
	return quoted[:1] + colorSecret(quoted[1:len(quoted)-1], true) + quoted[len(quoted)-1:]
}

func formatSendInstructions(secret, flags, webURL string, colorEnabled bool) string {
	return fmt.Sprintf(`Code is: %[1]s

On the other computer run:
(For Windows)
    croc %[2]s%[1]s
(For Linux/macOS)
    CROC_SECRET=%[3]s croc %[2]s

Or receive in a browser:
    %[4]s
`, colorSecret(secret, colorEnabled), flags, colorQuotedSecret(secret, colorEnabled), webURL)
}

func formatClipboardText(secret, flags string, extended bool) string {
	if !extended {
		return secret
	}
	return fmt.Sprintf("CROC_SECRET=%q croc %s", secret, strings.TrimSpace(flags))
}
