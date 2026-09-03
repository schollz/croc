package cli

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/stretchr/testify/require"
)

func TestSSHCommandDefaults(t *testing.T) {
	app := newApp()
	for _, command := range app.Commands {
		if command.Name != "ssh" {
			continue
		}
		command.Action = func(ctx *cli.Context) error {
			require.Equal(t, 12*time.Hour, ctx.Duration("duration"))
			require.Equal(t, 2*time.Minute, ctx.Duration("reconnect-window"))
			require.False(t, ctx.Bool("headless"))
			require.False(t, ctx.Bool("no-reconnect"))
			require.Equal(t, "auto", ctx.String("transport"))
			return nil
		}
		require.NoError(t, app.Run([]string{"croc", "ssh"}))
		return
	}
	t.Fatal("ssh command not found")
}

func TestFormatSSHJoinCommand(t *testing.T) {
	command := formatSSHJoinCommand("acid-acorn-acre-acts-ahead-alien", "", "pass123", false)
	if runtime.GOOS == "windows" {
		require.Equal(t, "croc ssh acid-acorn-acre-acts-ahead-alien", command)
	} else {
		require.Equal(t, "CROC_SECRET='acid-acorn-acre-acts-ahead-alien' croc ssh", command)
	}
	require.Equal(t, command, termui.Plain(formatSSHJoinCommand("acid-acorn-acre-acts-ahead-alien", "", "pass123", true)))
}

func TestFormatSSHJoinCommandIncludesCustomRelay(t *testing.T) {
	command := formatSSHJoinCommand("acid-acorn-acre-acts-ahead-alien", "relay.example:9009", "custom-pass", false)
	if runtime.GOOS == "windows" {
		require.Contains(t, command, `--relay "relay.example:9009"`)
		require.Contains(t, command, `--pass "custom-pass"`)
	} else {
		require.Contains(t, command, "CROC_RELAY='relay.example:9009'")
		require.Contains(t, command, "CROC_PASS='custom-pass'")
	}
}

func TestFormatSSHBrowserLinks(t *testing.T) {
	links := formatSSHBrowserLinks(
		"acid-acorn-acre-acts-ahead-alien",
		"badge-baker-basin-beach-beard-beast",
	)
	require.Equal(t, `  Browser read/write: https://getcroc.com/#ssh?code=acid-acorn-acre-acts-ahead-alien
  Browser read-only:  https://getcroc.com/#ssh?code=badge-baker-basin-beach-beard-beast
`, links)
	require.Equal(t,
		"https://getcroc.com/#ssh?code=word%2Fword+%3F%26",
		sshBrowserURL("word/word ?&"),
	)
}

func TestFormatShellAssignmentEscapesApostrophes(t *testing.T) {
	require.Equal(t, `CROC_PASS='don'\''t'\''panic'`, formatShellAssignment("CROC_PASS", "don't'panic", false))
}

func TestFormatUnixSSHCodeMessage(t *testing.T) {
	message := formatUnixSSHCodeMessage("acid-acorn-acre-acts-ahead-alien", "", "pass123", false)
	require.Contains(t, message, "process list")
	require.Contains(t, message, "CROC_SECRET='acid-acorn-acre-acts-ahead-alien' croc ssh")
	require.False(t, strings.Contains(message, "\x1b"))
}
