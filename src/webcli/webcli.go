// Package webcli implements the standalone croc-web server command.
package webcli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/publicrelay"
	storeapi "github.com/schollz/croc/v11/src/store"
	buildversion "github.com/schollz/croc/v11/src/version"
	"github.com/schollz/croc/v11/src/webassets"
	"github.com/schollz/croc/v11/src/webrelay"
	log "github.com/schollz/logger"
)

// Version specifies the version reported by croc-web.
var Version = buildversion.Value

// Run parses args, runs the web server, and stops it when ctx is cancelled.
func Run(ctx context.Context, args []string) error {
	runtime.GOMAXPROCS(runtime.NumCPU())
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 {
		args = []string{"croc-web"}
	}
	return newApp(ctx).Run(args)
}

func newApp(ctx context.Context) *cli.App {
	app := cli.NewApp()
	app.Name = "croc-web"
	app.Version = Version
	app.Compiled = time.Now()
	app.Usage = "serve the croc website and browser relay"
	app.UsageText = "croc-web [OPTIONS] [public-host[:port]]"
	app.ArgsUsage = "[public-host[:port]]"
	app.Flags = []cli.Flag{
		&cli.StringFlag{Name: "bind", Value: "127.0.0.1:9014", Usage: "local HTTP bind address"},
		&cli.StringFlag{Name: "relays", Aliases: []string{"relay"}, Value: strings.Join(publicrelay.Relays(), ","), Usage: "ordered comma-separated upstream croc relay hosts"},
		&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013,9014,9015,9016,9017", Usage: "allowed upstream relay ports"},
		&cli.StringFlag{Name: "pass", Value: models.DEFAULT_PASSPHRASE, Usage: "password for the relay", EnvVars: []string{"CROC_PASS"}},
		&cli.BoolFlag{Name: "debug", Usage: "enable debug logging"},
		&cli.StringFlag{Name: "store-dir", Usage: "enable encrypted temporary storage in this directory"},
		&cli.StringFlag{Name: "store-max-transfer", Value: "1GiB", Usage: "maximum plaintext bytes per stored transfer"},
		&cli.StringFlag{Name: "store-quota", Value: "5GiB", Usage: "maximum managed stored-transfer bytes"},
		&cli.StringFlag{Name: "store-min-free", Value: "512MiB", Usage: "disk space to keep free"},
		&cli.IntFlag{Name: "store-max-files", Value: 100, Usage: "maximum files per stored transfer"},
		&cli.IntFlag{Name: "store-downloads", Value: 1, Usage: "maximum verified downloads per stored transfer", EnvVars: []string{"CROC_STORE_DOWNLOADS"}},
		&cli.StringFlag{Name: "store-max-expiration", Value: "0", Usage: "maximum stored lifetime (m, h, d, or w; 0 is unlimited)", EnvVars: []string{"CROC_STORE_MAX_EXPIRATION"}},
		&cli.IntFlag{Name: "store-create-rate", Value: 5, Usage: "stored transfers created per client IP per hour"},
		&cli.IntFlag{Name: "store-active-uploads", Value: 2, Usage: "concurrent uploads per client IP"},
		&cli.StringSliceFlag{Name: "store-trusted-proxy", Usage: "trusted reverse-proxy CIDR for client IP forwarding"},
	}
	app.HideHelp = false
	app.HideVersion = false
	app.Action = func(c *cli.Context) error {
		return serve(ctx, c)
	}
	return app
}

func serve(ctx context.Context, c *cli.Context) error {
	if c.Args().Len() > 1 {
		return errors.New("croc-web accepts one public website address")
	}
	if c.Bool("debug") {
		log.SetLevel("debug")
		log.Debug("debug mode on")
	} else {
		log.SetLevel("info")
	}

	publicAddress := c.Args().First()
	bindAddress, origin, err := resolveServeAddress(
		publicAddress,
		c.String("bind"),
		c.IsSet("bind"),
	)
	if err != nil {
		return err
	}

	var storeService *storeapi.Service
	storeDirectory := strings.TrimSpace(c.String("store-dir"))
	if storeDirectory != "" {
		if c.Int("store-downloads") < 1 {
			return errors.New("--store-downloads must be positive")
		}
		maxExpiration, parseErr := storeapi.ParseExpiration(c.String("store-max-expiration"), true)
		if parseErr != nil {
			return fmt.Errorf("invalid --store-max-expiration: %w", parseErr)
		}
		maxTransfer, parseErr := parseByteSize(c.String("store-max-transfer"))
		if parseErr != nil {
			return fmt.Errorf("invalid --store-max-transfer: %w", parseErr)
		}
		maxTotal, parseErr := parseByteSize(c.String("store-quota"))
		if parseErr != nil {
			return fmt.Errorf("invalid --store-quota: %w", parseErr)
		}
		minFree, parseErr := parseByteSize(c.String("store-min-free"))
		if parseErr != nil {
			return fmt.Errorf("invalid --store-min-free: %w", parseErr)
		}
		var trusted []netip.Prefix
		for _, value := range c.StringSlice("store-trusted-proxy") {
			prefix, prefixErr := netip.ParsePrefix(strings.TrimSpace(value))
			if prefixErr != nil {
				return fmt.Errorf("invalid --store-trusted-proxy %q: %w", value, prefixErr)
			}
			trusted = append(trusted, prefix)
		}
		storeService, err = storeapi.New(storeapi.Config{
			Root:             storeDirectory,
			MaxTransferBytes: maxTransfer,
			MaxTotalBytes:    maxTotal,
			MinFreeBytes:     minFree,
			MaxFiles:         c.Int("store-max-files"),
			MaxDownloads:     c.Int("store-downloads"),
			MaxExpiration:    maxExpiration,
			CreatePerHour:    c.Int("store-create-rate"),
			MaxActiveUploads: c.Int("store-active-uploads"),
			TrustedProxies:   trusted,
		})
		if err != nil {
			return err
		}
		defer storeService.Close()
	}

	return webrelay.Run(ctx, webrelay.Config{
		ListenAddress:  bindAddress,
		PublicAddress:  origin,
		RelayHosts:     parseRelayHosts(c.String("relays")),
		RelayPassword:  determinePass(c.String("pass")),
		AllowedPorts:   parseRelayPorts(c.String("ports")),
		OriginPatterns: []string{origin},
		StaticFiles:    webassets.Files(),
		StoreService:   storeService,
		UmamiURL:       os.Getenv("UMAMI_URL"),
		UmamiWebsiteID: os.Getenv("UMAMI_WEBSITE_ID"),
		GoogleAdSense:  os.Getenv("GOOGLE_ADSENSE"),
		GoogleAdsTXT:   os.Getenv("GOOGLE_ADS_TXT"),
	})
}

func determinePass(value string) string {
	pass := value
	if contents, err := os.ReadFile(pass); err == nil {
		pass = string(contents)
	}
	return strings.TrimSpace(pass)
}

func parseRelayPorts(value string) []string {
	var ports []string
	for _, port := range strings.Split(value, ",") {
		if port = strings.TrimSpace(port); port != "" {
			ports = append(ports, port)
		}
	}
	return ports
}

func parseRelayHosts(value string) []string {
	var hosts []string
	for _, host := range strings.Split(value, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func parseByteSize(value string) (int64, error) {
	normalized := strings.TrimSpace(strings.ToUpper(value))
	if normalized == "" {
		return 0, errors.New("size cannot be empty")
	}
	multipliers := []struct {
		suffix     string
		multiplier int64
	}{
		{"TIB", 1 << 40},
		{"GIB", 1 << 30},
		{"MIB", 1 << 20},
		{"KIB", 1 << 10},
		{"TB", 1_000_000_000_000},
		{"GB", 1_000_000_000},
		{"MB", 1_000_000},
		{"KB", 1_000},
		{"B", 1},
	}
	multiplier := int64(1)
	number := normalized
	for _, candidate := range multipliers {
		if strings.HasSuffix(normalized, candidate.suffix) {
			multiplier = candidate.multiplier
			number = strings.TrimSpace(strings.TrimSuffix(normalized, candidate.suffix))
			break
		}
	}
	parsed, err := strconv.ParseInt(number, 10, 64)
	if err != nil || parsed < 0 || (parsed > 0 && parsed > (1<<63-1)/multiplier) {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	return parsed * multiplier, nil
}

func resolveServeAddress(publicAddress, bindAddress string, bindExplicit bool) (string, string, error) {
	publicAddress = strings.TrimSpace(publicAddress)
	publicExplicit := publicAddress != ""
	if publicAddress == "" {
		publicAddress = "localhost:5173"
	}
	if strings.Contains(publicAddress, "://") || strings.ContainsAny(publicAddress, "/?#") {
		return "", "", fmt.Errorf("website address must be a host or host:port: %q", publicAddress)
	}

	host, port, err := net.SplitHostPort(publicAddress)
	if err != nil {
		if strings.Contains(publicAddress, ":") {
			return "", "", fmt.Errorf("invalid website address %q: %w", publicAddress, err)
		}
		host = publicAddress
		port = ""
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return "", "", fmt.Errorf("invalid website host %q", host)
	}
	if port != "" {
		portNumber, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || portNumber == 0 {
			return "", "", fmt.Errorf("invalid website port %q", port)
		}
	}

	bindAddress = strings.TrimSpace(bindAddress)
	if bindAddress == "" {
		bindAddress = "127.0.0.1:9014"
	}
	if publicExplicit && !bindExplicit && port != "" {
		ip := net.ParseIP(host)
		if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
			bindAddress = publicAddress
		}
	}
	if _, bindPort, splitErr := net.SplitHostPort(bindAddress); splitErr != nil {
		return "", "", fmt.Errorf("invalid bind address %q: %w", bindAddress, splitErr)
	} else if portNumber, parseErr := strconv.ParseUint(bindPort, 10, 16); parseErr != nil || portNumber == 0 {
		return "", "", fmt.Errorf("invalid bind port %q", bindPort)
	}

	return bindAddress, publicAddress, nil
}
