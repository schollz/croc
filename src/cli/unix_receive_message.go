package cli

import (
	"fmt"
	"strings"

	"github.com/schollz/croc/v11/src/termui"
)

func formatUnixReceiveCodeMessage(secret string, colorEnabled bool) string {
	shellSecret := strings.ReplaceAll(secret, "'", `'\''`)
	environmentCommand := termui.Color("CROC_SECRET='", termui.Cyan, colorEnabled) +
		termui.Secret(shellSecret, colorEnabled) +
		termui.Color("' croc", termui.Cyan, colorEnabled)

	return fmt.Sprintf(`%s

Receive more securely with the code you entered:

  %s

Or enter it interactively:

  %s
  Enter receive code: %s

To allow command-line codes again, enable classic mode:

  %s

`,
		"For security, croc does not accept receive codes on the UNIX\ncommand line because they can appear in the process list.",
		environmentCommand,
		termui.Color("croc", termui.Cyan, colorEnabled),
		termui.Secret(secret, colorEnabled),
		termui.Color("croc --classic", termui.Cyan, colorEnabled),
	)
}
