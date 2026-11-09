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

// shellQuote returns s quoted for a POSIX shell when it contains characters a
// shell would interpret, so the printed receive command can be pasted as is.
// Plain values such as relay addresses and generated codes are left untouched.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./_-", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func formatSendInstructions(secret, flags, webURL, clipboardNotice string, colorEnabled bool) string {
	if clipboardNotice != "" {
		clipboardNotice = " (" + clipboardNotice + ")"
	}
	instructions := fmt.Sprintf(`On the other computer, run:
  croc %s%s%s
`, flags, colorSecret(shellQuote(secret), colorEnabled), clipboardNotice)
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
