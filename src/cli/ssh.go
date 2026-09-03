package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/comm"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/sshshare"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/utils"
)

func sshSession(c *cli.Context) error {
	setDebugLevel(c)
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")
	if c.Args().Present() || strings.TrimSpace(os.Getenv("CROC_SECRET")) != "" {
		return joinSSHSession(c)
	}
	return hostSSHSession(c)
}

func sshRelay(c *cli.Context) (string, error) {
	if c.IsSet("relay") && c.IsSet("relay6") {
		return "", errors.New("croc ssh accepts only one explicit relay")
	}
	if c.IsSet("relay") {
		return strings.TrimSpace(c.String("relay")), nil
	}
	if c.IsSet("relay6") {
		return strings.TrimSpace(c.String("relay6")), nil
	}
	return "", nil
}

func joinSSHSession(c *cli.Context) error {
	relay, err := sshRelay(c)
	if err != nil {
		return err
	}
	relayPassword := determinePass(c)
	secret := strings.TrimSpace(os.Getenv("CROC_SECRET"))
	commandLineSecret := strings.Join(c.Args().Slice(), "-")
	classic := utils.Exists(getClassicConfigFile(true))
	if secret == "" && commandLineSecret != "" {
		if runtime.GOOS != "windows" && !classic {
			output, colorEnabled := termui.Output(os.Stdout)
			fmt.Fprint(output, formatUnixSSHCodeMessage(commandLineSecret, relay, relayPassword, colorEnabled))
			return nil
		}
		secret = commandLineSecret
	}
	if secret == "" {
		var err error
		secret, err = utils.GetInput("Enter SSH code: ")
		if err != nil {
			return fmt.Errorf("could not read SSH code: %w", err)
		}
	}
	return sshshare.Join(c.Context, sshshare.ClientConfig{
		Code:            secret,
		RelayAddress:    relay,
		RelayPassword:   relayPassword,
		Curve:           c.String("curve"),
		Input:           os.Stdin,
		Output:          os.Stdout,
		ErrorOutput:     os.Stderr,
		Terminal:        os.Stdin,
		Reconnect:       !c.Bool("no-reconnect"),
		ReconnectWindow: c.Duration("reconnect-window"),
		TransportMode:   sshshare.TransportMode(c.String("transport")),
		OnEvent: func(event sshshare.JoinEvent) {
			switch event.State {
			case sshshare.JoinStateConnected:
				path := ""
				if event.Transport == sshshare.TransportRelay {
					path = " via the croc relay"
				}
				fmt.Fprintf(os.Stderr, "Connected with %s access%s. Press Ctrl-] to detach.\r\n", event.Role, path)
			case sshshare.JoinStateReconnecting:
				fmt.Fprintf(os.Stderr, "\r\nConnection lost; reconnecting…\r\n")
			}
		},
		Logf: func(format string, args ...any) { log.Debugf(format, args...) },
	})
}

func hostSSHSession(c *cli.Context) error {
	duration := c.Duration("duration")
	if duration <= 0 {
		return errors.New("SSH session duration must be positive")
	}
	relay, err := sshRelay(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context, duration)
	defer cancel()
	relayPassword := determinePass(c)
	host, err := sshshare.StartHost(ctx, sshshare.HostConfig{
		RelayAddress:  relay,
		RelayPassword: relayPassword,
		Directory:     c.String("dir"),
		Logf:          func(format string, args ...any) { log.Debugf(format, args...) },
	})
	if err != nil {
		return err
	}
	defer host.Close()

	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprintln(output, termui.Emphasis("Shared SSH terminal is ready", colorEnabled))
	fmt.Fprintf(output, "  Read/write: %s\n", formatSSHJoinCommand(host.Code(sshshare.RoleReadWrite), relay, relayPassword, colorEnabled))
	fmt.Fprintf(output, "  Read-only:  %s\n", formatSSHJoinCommand(host.Code(sshshare.RoleReadOnly), relay, relayPassword, colorEnabled))
	fmt.Fprintf(output, "  Expires:    %s\n", duration)
	fmt.Fprintln(output, "  Stop:       Ctrl-C")
	if c.Bool("headless") {
		fmt.Fprintln(output, "Waiting for participants…")
		return host.Wait()
	}
	fmt.Fprintln(output, "  Detach:     Ctrl-] (the shared shell keeps running)")

	err = host.AttachLocalTerminal(ctx, os.Stdin, os.Stdout)
	if errors.Is(err, sshshare.ErrDetached) {
		fmt.Fprintln(os.Stderr, "\nDetached; the shared shell is still running. Press Ctrl-C to stop it.")
		return host.Wait()
	}
	return err
}

func formatSSHJoinCommand(secret, relay, relayPassword string, colorEnabled bool) string {
	if runtime.GOOS == "windows" {
		command := "croc"
		if relay != "" {
			command += ` --relay "` + strings.ReplaceAll(relay, `"`, `\"`) + `"`
		}
		if relayPassword != models.DEFAULT_PASSPHRASE {
			command += ` --pass "` + strings.ReplaceAll(relayPassword, `"`, `\"`) + `"`
		}
		return termui.Color(command+" ssh ", termui.Cyan, colorEnabled) + termui.Secret(secret, colorEnabled)
	}
	assignments := []string{formatShellAssignment("CROC_SECRET", secret, colorEnabled)}
	if relay != "" {
		assignments = append(assignments, formatShellAssignment("CROC_RELAY", relay, colorEnabled))
	}
	if relayPassword != models.DEFAULT_PASSPHRASE {
		assignments = append(assignments, formatShellAssignment("CROC_PASS", relayPassword, colorEnabled))
	}
	return strings.Join(assignments, " ") + termui.Color(" croc ssh", termui.Cyan, colorEnabled)
}

func formatShellAssignment(name, value string, colorEnabled bool) string {
	shellValue := strings.ReplaceAll(value, "'", `'\''`)
	return termui.Color(name+"='", termui.Cyan, colorEnabled) +
		termui.Secret(shellValue, colorEnabled) + termui.Color("'", termui.Cyan, colorEnabled)
}

func formatUnixSSHCodeMessage(secret, relay, relayPassword string, colorEnabled bool) string {
	return fmt.Sprintf(`For security, croc does not accept SSH codes on the UNIX
command line because they can appear in the process list.

Join securely with:

  %s

Or run %s and paste the code when prompted.

`, formatSSHJoinCommand(secret, relay, relayPassword, colorEnabled), termui.Color("croc ssh", termui.Cyan, colorEnabled))
}
