package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/tunnel"
	"github.com/schollz/croc/v11/src/utils"
)

func tunnelSession(c *cli.Context) error {
	setDebugLevel(c)
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")
	if c.NArg() > 1 {
		return errors.New("croc tunnel accepts one local port or invitation")
	}
	relay, err := sshRelay(c)
	if err != nil {
		return errors.New("croc tunnel accepts only one explicit relay")
	}
	secret := strings.TrimSpace(os.Getenv("CROC_SECRET"))
	arg := c.Args().First()
	port, portErr := strconv.Atoi(arg)
	if arg != "" && portErr == nil {
		if secret != "" || c.IsSet("local-port") {
			return errors.New("hosting a port cannot be combined with an invitation or --local-port")
		}
		if c.Duration("duration") <= 0 {
			return errors.New("tunnel duration must be positive")
		}
		base, err := url.Parse(c.String("web-url"))
		if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			return errors.New("--web-url must be an HTTP(S) website URL without credentials, query or fragment")
		}
		ctx, cancel := context.WithTimeout(c.Context, c.Duration("duration"))
		defer cancel()
		host, err := tunnel.StartHost(ctx, tunnel.HostConfig{Port: port, RelayAddress: relay, RelayPassword: determinePass(c), Logf: log.Debugf})
		if err != nil {
			return err
		}
		defer host.Close()
		base.Fragment = "tunnel?code=" + host.Code()
		fmt.Fprintf(os.Stderr, "Sharing localhost:%d\n  CLI:     %s\n  Browser: %s\n  Expires: %s\nPress Ctrl-C to stop sharing.\n", port, formatTunnelJoinCommand(host.Code(), relay, determinePass(c), false), base.String(), c.Duration("duration"))
		return host.Wait()
	}
	if arg != "" {
		if secret != "" && arg != secret {
			return errors.New("invitation argument conflicts with CROC_SECRET")
		}
		if _, err := codephrase.ParseTunnel(arg); err != nil {
			return err
		}
		if runtime.GOOS != "windows" && !utils.Exists(getClassicConfigFile(true)) && secret == "" {
			fmt.Fprintln(os.Stderr, "Join with:", formatTunnelJoinCommand(arg, relay, determinePass(c), false))
			return nil
		}
		secret = arg
	}
	if secret == "" {
		secret, err = utils.GetInputContext(c.Context, "Enter tunnel code: ")
		if err != nil {
			return err
		}
	}
	localPort := c.Int("local-port")
	if localPort < 0 || localPort > 65535 || (c.IsSet("local-port") && localPort == 0) {
		return errors.New("--local-port must be between 1 and 65535")
	}
	err = tunnel.Forward(c.Context, tunnel.ClientConfig{Code: secret, RelayAddress: relay, RelayPassword: determinePass(c), Curve: c.String("curve")}, localPort, func(status string) { fmt.Fprintln(os.Stderr, status) })
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("tunnel: %w", err)
	}
	fmt.Fprintln(os.Stderr, "The host ended the tunnel.")
	return nil
}

func formatTunnelJoinCommand(secret, relay, relayPassword string, colorEnabled bool) string {
	if runtime.GOOS == "windows" {
		command := "croc"
		if relay != "" {
			command += ` --relay "` + strings.ReplaceAll(relay, `"`, `\"`) + `"`
		}
		if relayPassword != models.DEFAULT_PASSPHRASE {
			command += ` --pass "` + strings.ReplaceAll(relayPassword, `"`, `\"`) + `"`
		}
		return termui.Color(command+" tunnel ", termui.Cyan, colorEnabled) + termui.Secret(secret, colorEnabled)
	}
	assignments := []string{formatShellAssignment("CROC_SECRET", secret, colorEnabled)}
	if relay != "" {
		assignments = append(assignments, formatShellAssignment("CROC_RELAY", relay, colorEnabled))
	}
	if relayPassword != models.DEFAULT_PASSPHRASE {
		assignments = append(assignments, formatShellAssignment("CROC_PASS", relayPassword, colorEnabled))
	}
	return strings.Join(assignments, " ") + termui.Color(" croc tunnel", termui.Cyan, colorEnabled)
}
