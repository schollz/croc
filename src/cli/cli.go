package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/croc/v11/internal/cli"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/croc"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/publicrelay"
	"github.com/schollz/croc/v11/src/storeclient"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/utils"
	buildversion "github.com/schollz/croc/v11/src/version"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"
)

// Version specifies the version
var Version = buildversion.Value

// Run will run the command line program
func Run() (err error) {
	// use all of the processors
	runtime.GOMAXPROCS(runtime.NumCPU())

	return newApp().Run(os.Args)
}

func newApp() *cli.App {
	app := cli.NewApp()
	app.Name = "croc"
	app.Version = Version
	app.Compiled = time.Now()
	app.Usage = "easily and securely transfer stuff from one computer to another"
	app.UsageText = `croc [GLOBAL OPTIONS] [COMMAND] [COMMAND OPTIONS] [filename(s) or folder]

   USAGE EXAMPLES:
   Send a file:
      croc send file.txt

      -git to respect your .gitignore
   Send multiple files:
      croc send file1.txt file2.txt file3.txt
    or
      croc send *.jpg

   Send everything in a folder:
      croc send example-folder-name

   Send a file with a custom code:
      croc send --code secret-code file.txt

   Receive a file using code:
      croc secret-code`
	app.Commands = []*cli.Command{
		{
			Name:        "send",
			Usage:       "send file(s), or folder (see options with croc send -h)",
			Description: "send file(s), or folder, over the relay",
			ArgsUsage:   "[filename(s) or folder]",
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: "zip", Usage: "zip folder before sending"},
				&cli.StringFlag{Name: "code", Aliases: []string{"c"}, Usage: "codephrase used to connect to relay (at least 6 characters)"},
				&cli.StringFlag{Name: "hash", Value: "xxhash", Usage: "hash algorithm (xxhash, imohash, md5, highway)"},
				&cli.StringFlag{Name: "text", Aliases: []string{"t"}, Usage: "send some text"},
				&cli.BoolFlag{Name: "no-local", Usage: "disable local relay when sending"},
				&cli.BoolFlag{Name: "no-multi", Usage: "disable multiplexing"},
				&cli.BoolFlag{Name: "git", Usage: "enable .gitignore respect / don't send ignored files"},
				&cli.IntFlag{Name: "port", Value: 9009, Usage: "base port for the relay"},
				&cli.IntFlag{Name: "transfers", Value: 4, Usage: "number of ports to use for transfers"},
				&cli.BoolFlag{Name: "qrcode", Aliases: []string{"qr"}, Usage: "show the web receive URL as a qrcode"},
				&cli.StringFlag{Name: "exclude", Value: "", Usage: "exclude files if they contain any of the comma separated strings"},
				&cli.StringFlag{Name: "exclude-file", Value: "", Usage: "exclude files matching any of the comma separated relative paths exactly"},
				&cli.StringFlag{Name: "socks5", Value: "", Usage: "add a socks5 proxy", EnvVars: []string{"SOCKS5_PROXY"}},
				&cli.StringFlag{Name: "connect", Value: "", Usage: "add a http proxy", EnvVars: []string{"HTTP_PROXY"}},
				&cli.BoolFlag{Name: "store", Usage: "upload encrypted files for a finite lifetime or a limited number of verified downloads"},
				&cli.IntFlag{Name: "store-downloads", Value: 1, Usage: "number of verified downloads allowed in stored mode"},
				&cli.StringFlag{Name: "store-expiration", Value: "1d", Usage: "stored lifetime after upload (for example 90m, 12h, 3d, or 2w)"},
				&cli.StringFlag{Name: "store-url", Value: "https://getcroc.com", Usage: "stored-transfer service origin", EnvVars: []string{"CROC_STORE_URL"}},
			},
			HelpName: "croc send",
			Action:   send,
		},
		{
			Name:        "relay",
			Usage:       "start your own relay (optional)",
			Description: "start relay",
			HelpName:    "croc relay",
			Action:      relay,
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "host", Usage: "host of the relay"},
				&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the relay", EnvVars: []string{"CROC_PORTS"}},
				&cli.IntFlag{Name: "port", Value: 9009, Usage: "base port for the relay", EnvVars: []string{"CROC_PORT"}},
				&cli.IntFlag{Name: "transfers", Value: 5, Usage: "number of ports to use for relay"},
				&cli.IntFlag{Name: "max-rooms-open", Value: tcp.DEFAULT_MAX_ROOMS_OPEN, Usage: "maximum waiting rooms per relay port", EnvVars: []string{"CROC_MAX_ROOMS_OPEN"}},
				&cli.IntFlag{Name: "max-pending-handshakes", Value: tcp.DEFAULT_MAX_PENDING_HANDSHAKES, Usage: "maximum incomplete handshakes per relay port", EnvVars: []string{"CROC_MAX_PENDING_HANDSHAKES"}},
				&cli.DurationFlag{Name: "handshake-timeout", Value: tcp.DEFAULT_HANDSHAKE_TIMEOUT, Usage: "maximum time for an initial relay handshake", EnvVars: []string{"CROC_HANDSHAKE_TIMEOUT"}},
			},
		},
		{
			Name:   "generate-fish-completion",
			Usage:  "generate fish completion and output to stdout",
			Hidden: true,
			Action: func(ctx *cli.Context) error {
				completion, err := ctx.App.ToFishCompletion()
				if err != nil {
					return err
				}
				fmt.Print(completion)
				return nil
			},
		},
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{Name: "internal-dns", Usage: "use a built-in DNS stub resolver rather than the host operating system"},
		&cli.BoolFlag{Name: "classic", Usage: "toggle between the classic mode (insecure due to local attack vector) and new mode (secure)"},
		&cli.BoolFlag{Name: "remember", Usage: "save these settings to reuse next time"},
		&cli.BoolFlag{Name: "debug", Usage: "toggle debug mode"},
		&cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		&cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		&cli.BoolFlag{Name: "no-compress", Usage: "disable compression"},
		&cli.BoolFlag{Name: "ask", Usage: "make sure sender and recipient are prompted"},
		&cli.BoolFlag{Name: "local", Usage: "force to use only local connections"},
		&cli.BoolFlag{Name: "ignore-stdin", Usage: "ignore piped stdin"},
		&cli.BoolFlag{Name: "overwrite", Usage: "do not prompt to overwrite or resume"},
		&cli.BoolFlag{Name: "rename", Usage: "receive files that already exist under a new name instead of prompting"},
		&cli.BoolFlag{Name: "testing", Usage: "flag for testing purposes"},
		&cli.BoolFlag{Name: "quiet", Usage: "disable all output"},
		&cli.BoolFlag{Name: "disable-clipboard", Usage: "disable copy to clipboard"},
		&cli.BoolFlag{Name: "extended-clipboard", Usage: "copy full command with secret as env variable to clipboard"},
		&cli.StringFlag{Name: "revoke", Usage: "revoke a stored transfer using its local sender receipt"},
		&cli.StringFlag{Name: "multicast", Value: "239.255.255.250", Usage: "multicast address to use for local discovery"},
		&cli.StringFlag{Name: "curve", Value: "p256", Usage: "choose an encryption curve (" + strings.Join(pake.AvailableCurves(), ", ") + ")"},
		&cli.StringFlag{Name: "ip", Value: "", Usage: "set sender ip if known e.g. 10.0.0.1:9009, [::1]:9009"},
		&cli.StringFlag{Name: "relay", Value: models.DEFAULT_RELAY, Usage: "address of the relay", EnvVars: []string{"CROC_RELAY"}},
		&cli.StringFlag{Name: "relay6", Value: models.DEFAULT_RELAY6, Usage: "ipv6 address of the relay", EnvVars: []string{"CROC_RELAY6"}},
		&cli.StringFlag{Name: "out", Value: ".", Usage: "specify an output folder to receive the file"},
		&cli.StringFlag{Name: "pass", Value: models.DEFAULT_PASSPHRASE, Usage: "password for the relay", EnvVars: []string{"CROC_PASS"}},
		&cli.StringFlag{Name: "socks5", Value: "", Usage: "add a socks5 proxy", EnvVars: []string{"SOCKS5_PROXY"}},
		&cli.StringFlag{Name: "connect", Value: "", Usage: "add a http proxy", EnvVars: []string{"HTTP_PROXY"}},
		&cli.StringFlag{Name: "throttleUpload", Value: "", Usage: "throttle the upload speed e.g. 500k"},
	}
	app.EnableBashCompletion = true
	app.HideHelp = false
	app.HideVersion = false
	app.Action = func(c *cli.Context) error {
		if c.Args().First() == "serve" {
			return errors.New("the web server has moved to the standalone croc-web binary")
		}
		if c.IsSet("revoke") {
			return revokeStored(c, c.String("revoke"))
		}

		allStringsAreFiles := func(strs []string) bool {
			for _, str := range strs {
				if !utils.Exists(str) {
					return false
				}
			}
			return true
		}

		// check if "classic" is set
		classicFile := getClassicConfigFile(true)
		classicInsecureMode := utils.Exists(classicFile)
		if c.Bool("classic") {
			if classicInsecureMode {
				// classic mode not enabled
				fmt.Print(`Classic mode is currently ENABLED.

Disabling this mode will prevent the shared secret from being visible
on the host's process list when passed via the command line. On a
multi-user system, this will help ensure that other local users cannot
access the shared secret and receive the files instead of the intended
recipient.

Do you wish to continue to DISABLE the classic mode? (y/N) `)
				choice, _ := utils.GetInput("")
				choice = strings.ToLower(choice)
				if choice == "y" || choice == "yes" {
					os.Remove(classicFile)
					fmt.Print("\nClassic mode DISABLED.\n\n")
					fmt.Print(`To send and receive, export the CROC_SECRET variable with the code phrase:

  Send:    CROC_SECRET=*** croc send file.txt

  Receive: CROC_SECRET=*** croc` + "\n\n")
				} else {
					fmt.Print("\nClassic mode ENABLED.\n")

				}
			} else {
				// enable classic mode
				// touch the file
				fmt.Print(`Classic mode is currently DISABLED.

Please note that enabling this mode will make the shared secret visible
on the host's process list when passed via the command line. On a
multi-user system, this could allow other local users to access the
shared secret and receive the files instead of the intended recipient.

Do you wish to continue to enable the classic mode? (y/N) `)
				choice, _ := utils.GetInput("")
				choice = strings.ToLower(choice)
				if choice == "y" || choice == "yes" {
					fmt.Print("\nClassic mode ENABLED.\n\n")
					os.WriteFile(classicFile, []byte("enabled"), 0o644)
					fmt.Print(`To send and receive, use the code phrase:

  Send:    croc send --code *** file.txt

  Receive: croc ***` + "\n\n")
				} else {
					fmt.Print("\nClassic mode DISABLED.\n")
				}
			}
			os.Exit(0)
		}

		// if trying to send but forgot send, let the user know
		if c.Args().Present() && allStringsAreFiles(c.Args().Slice()) {
			fnames := []string{}
			for _, fpath := range c.Args().Slice() {
				_, basename := filepath.Split(fpath)
				fnames = append(fnames, "'"+basename+"'")
			}
			promptMessage := fmt.Sprintf("Did you mean to send %s? (Y/n) ", strings.Join(fnames, ", "))
			choice, errInput := utils.GetInput(promptMessage)
			if errInput != nil {
				return fmt.Errorf("could not read confirmation (use 'croc send' to send without one): %w", errInput)
			}
			choice = strings.ToLower(choice)
			if choice == "" || choice == "y" || choice == "yes" {
				return send(c)
			}
		}

		return receive(c)
	}

	return app
}

func setDebugLevel(c *cli.Context) {
	if c.Bool("quiet") {
		log.SetLevel("error")
	} else if c.Bool("debug") {
		log.SetLevel("debug")
		log.Debug("debug mode on")
		// print the public IP address
		ip, err := utils.PublicIP()
		if err == nil {
			log.Debugf("public IP address: %s", ip)
		} else {
			log.Debug(err)
		}

	} else {
		log.SetLevel("info")
	}
}

func getSendConfigFile(requireValidPath bool) string {
	configFile, err := utils.GetConfigDir(requireValidPath)
	if err != nil {
		log.Error(err)
		return ""
	}
	return path.Join(configFile, "send.json")
}

func getClassicConfigFile(requireValidPath bool) string {
	configFile, err := utils.GetConfigDir(requireValidPath)
	if err != nil {
		log.Error(err)
		return ""
	}
	return path.Join(configFile, "classic_enabled")
}

func getBestRelayCacheFile(requireValidPath bool) (string, error) {
	configDir, err := utils.GetConfigDir(requireValidPath)
	if err != nil {
		return "", err
	}
	return path.Join(configDir, "best-relay"), nil
}

func getReceiveConfigFile(requireValidPath bool) (string, error) {
	configFile, err := utils.GetConfigDir(requireValidPath)
	if err != nil {
		log.Error(err)
		return "", err
	}
	return path.Join(configFile, "receive.json"), nil
}

func determinePass(c *cli.Context) (pass string) {
	pass = c.String("pass")
	b, err := os.ReadFile(pass)
	if err == nil {
		pass = string(b)
	}
	pass = strings.TrimSpace(pass)
	return
}

func resolveSendSharedSecret(sharedSecret, envSecret string) string {
	if envSecret != "" {
		return envSecret
	}
	return sharedSecret
}

func usesPublicRelay(c *cli.Context, options croc.Options) bool {
	return !c.IsSet("relay") &&
		!c.IsSet("relay6") &&
		!options.OnlyLocal &&
		options.IP == "" &&
		options.RelayAddress == models.DEFAULT_RELAY &&
		options.RelayAddress6 == models.DEFAULT_RELAY6
}

func assignPublicRelay(options *croc.Options, relayIndex int) error {
	relays := publicrelay.Relays()
	if relayIndex < 0 || relayIndex >= len(relays) {
		return codephrase.ErrInvalidRelayIndex
	}
	options.RelayAddress = relays[relayIndex]
	options.RelayAddress6 = ""
	options.PublicRelay = true
	log.Debugf("public relay index %d selected: %s", relayIndex, options.RelayAddress)
	return nil
}

func assignPublicRelayForCode(options *croc.Options) error {
	relays := publicrelay.Relays()
	relayIndex, err := codephrase.RelayIndex(options.SharedSecret, len(relays))
	if err != nil {
		return err
	}
	log.Debugf("code maps to public relay index %d", relayIndex)
	return assignPublicRelay(options, relayIndex)
}

func selectBestPublicRelay(probe publicrelay.Probe) (int, error) {
	relays := publicrelay.Relays()
	best, duration, err := publicrelay.SelectFirst(
		context.Background(),
		relays,
		publicrelay.ProbeTimeout,
		probe,
	)
	if err == nil {
		log.Debugf("public relay %d (%s) won probe race in %s", best, relays[best], duration)
	}
	return best, err
}

func loadBestPublicRelay(relays []string) (int, error) {
	cacheFile, err := getBestRelayCacheFile(false)
	if err != nil {
		return 0, err
	}
	contents, err := os.ReadFile(cacheFile)
	if err != nil {
		return 0, err
	}
	address := string(contents)
	for index, relay := range relays {
		if address == relay {
			log.Debugf("using cached public relay %d (%s) from %s", index, address, cacheFile)
			return index, nil
		}
	}
	return 0, fmt.Errorf("cached public relay %q is not in the configured pool", address)
}

func saveBestPublicRelay(address string) error {
	cacheFile, err := getBestRelayCacheFile(true)
	if err != nil {
		return err
	}
	if err = writePrivateConfigFile(cacheFile, []byte(address)); err != nil {
		return err
	}
	log.Debugf("cached public relay %s in %s", address, cacheFile)
	return nil
}

func clearBestPublicRelay() {
	cacheFile, err := getBestRelayCacheFile(false)
	if err != nil {
		log.Debugf("could not locate public relay cache: %v", err)
		return
	}
	if err = os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
		log.Warnf("could not clear public relay cache %s: %v", cacheFile, err)
		return
	}
	log.Debugf("cleared public relay cache %s", cacheFile)
}

func clearBestPublicRelayOnSendError(generatedPublicCode bool, err error) {
	if generatedPublicCode && errors.Is(err, croc.ErrRelayConnection) {
		clearBestPublicRelay()
	}
}

func selectPublicRelay(probe publicrelay.Probe) (int, error) {
	relays := publicrelay.Relays()
	if relayIndex, err := loadBestPublicRelay(relays); err == nil {
		return relayIndex, nil
	} else if !os.IsNotExist(err) {
		log.Debugf("ignoring invalid public relay cache: %v", err)
	}

	relayIndex, err := selectBestPublicRelay(probe)
	if err != nil {
		return 0, err
	}
	if err = saveBestPublicRelay(relays[relayIndex]); err != nil {
		log.Warnf("could not cache public relay selection: %v", err)
	}
	return relayIndex, nil
}

func shouldExitForUnixSendCode(goos string, codeFlagSet, classicInsecureMode bool, envSecret string) bool {
	return goos != "windows" && codeFlagSet && !classicInsecureMode && envSecret == ""
}

func applyRememberedSendOptions(c *cli.Context, options *croc.Options, remembered croc.Options) {
	// Update anything that isn't explicitly set.
	if !c.IsSet("no-local") {
		options.DisableLocal = remembered.DisableLocal
	}
	if !c.IsSet("ports") && len(remembered.RelayPorts) > 0 {
		options.RelayPorts = remembered.RelayPorts
	}
	if !c.IsSet("code") {
		options.SharedSecret = remembered.SharedSecret
	}
	if !c.IsSet("pass") && remembered.RelayPassword != "" {
		options.RelayPassword = remembered.RelayPassword
	}
	if !c.IsSet("overwrite") {
		options.Overwrite = remembered.Overwrite
	}
	if !c.IsSet("rename") {
		options.Rename = remembered.Rename
	}
	if !c.IsSet("curve") && remembered.Curve != "" {
		options.Curve = remembered.Curve
	}
	if !c.IsSet("local") {
		options.OnlyLocal = remembered.OnlyLocal
	}
	if !c.IsSet("hash") {
		options.HashAlgorithm = remembered.HashAlgorithm
	}
	if !c.IsSet("git") {
		options.GitIgnore = remembered.GitIgnore
	}
	if !c.IsSet("disable-clipboard") {
		options.DisableClipboard = remembered.DisableClipboard
	}
	if !c.IsSet("relay") && strings.HasPrefix(remembered.RelayAddress, "non-default:") {
		rememberedAddr := strings.TrimPrefix(remembered.RelayAddress, "non-default:")
		options.RelayAddress = strings.TrimSpace(rememberedAddr)
	}
	if !c.IsSet("relay6") && strings.HasPrefix(remembered.RelayAddress6, "non-default:") {
		rememberedAddr := strings.TrimPrefix(remembered.RelayAddress6, "non-default:")
		options.RelayAddress6 = strings.TrimSpace(rememberedAddr)
	}
}

// parseRelayPorts splits a comma-separated --ports value, trimming whitespace
// around each entry and dropping empties. This keeps "9009, 9010," working the
// same as "9009,9010" instead of producing invalid port strings like " 9010".
func parseRelayPorts(portsFlag string) []string {
	var ports []string
	for _, p := range strings.Split(portsFlag, ",") {
		if p = strings.TrimSpace(p); p != "" {
			ports = append(ports, p)
		}
	}
	return ports
}

func send(c *cli.Context) (err error) {
	setDebugLevel(c)
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")
	if c.Bool("store") {
		return sendStored(c)
	}

	portParam := c.Int("port")
	if portParam == 0 {
		portParam = 9009
	}
	transfersParam := c.Int("transfers")
	if transfersParam == 0 {
		transfersParam = 4
	}
	excludeStrings := []string{}
	for _, v := range strings.Split(c.String("exclude"), ",") {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			excludeStrings = append(excludeStrings, v)
		}
	}
	excludeFiles := []string{}
	for _, v := range strings.Split(c.String("exclude-file"), ",") {
		v = utils.NormalizeRelativePath(strings.TrimSpace(v))
		if v != "" && v != "." {
			excludeFiles = append(excludeFiles, v)
		}
	}

	ports := make([]string, transfersParam+1)
	for i := 0; i <= transfersParam; i++ {
		ports[i] = strconv.Itoa(portParam + i)
	}

	crocOptions := croc.Options{
		SharedSecret:      c.String("code"),
		IsSender:          true,
		Debug:             c.Bool("debug"),
		NoPrompt:          c.Bool("yes"),
		RelayAddress:      c.String("relay"),
		RelayAddress6:     c.String("relay6"),
		Stdout:            c.Bool("stdout"),
		DisableLocal:      c.Bool("no-local"),
		OnlyLocal:         c.Bool("local"),
		IgnoreStdin:       c.Bool("ignore-stdin"),
		RelayPorts:        ports,
		Ask:               c.Bool("ask"),
		NoMultiplexing:    c.Bool("no-multi"),
		RelayPassword:     determinePass(c),
		SendingText:       c.String("text") != "",
		NoCompress:        c.Bool("no-compress"),
		Overwrite:         c.Bool("overwrite"),
		Rename:            c.Bool("rename"),
		Curve:             c.String("curve"),
		HashAlgorithm:     c.String("hash"),
		ThrottleUpload:    c.String("throttleUpload"),
		ZipFolder:         c.Bool("zip"),
		GitIgnore:         c.Bool("git"),
		ShowQrCode:        c.Bool("qrcode"),
		MulticastAddress:  c.String("multicast"),
		Exclude:           excludeStrings,
		ExcludeFile:       excludeFiles,
		Quiet:             c.Bool("quiet"),
		DisableClipboard:  c.Bool("disable-clipboard"),
		ExtendedClipboard: c.Bool("extended-clipboard"),
	}
	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}
	b, errOpen := os.ReadFile(getSendConfigFile(false))
	if errOpen == nil && !c.Bool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		applyRememberedSendOptions(c, &crocOptions, rememberedOptions)
	}
	publicRelayMode := usesPublicRelay(c, crocOptions)

	var fnames []string
	stat, _ := os.Stdin.Stat()
	if ((stat.Mode() & os.ModeCharDevice) == 0) && !c.Bool("ignore-stdin") {
		fnames, err = getStdin()
		if err != nil {
			return
		}
		utils.MarkFileForRemoval(fnames[0])
		defer func() {
			e := os.Remove(fnames[0])
			if e != nil {
				log.Error(e)
			}
		}()
	} else if c.String("text") != "" {
		fnames, err = makeTempFileWithString(c.String("text"))
		if err != nil {
			return
		}
		utils.MarkFileForRemoval(fnames[0])
		defer func() {
			e := os.Remove(fnames[0])
			if e != nil {
				log.Error(e)
			}
		}()

	} else {
		fnames = c.Args().Slice()
	}
	if len(fnames) == 0 {
		return errors.New("must specify file: croc send [filename(s) or folder]")
	}

	classicInsecureMode := utils.Exists(getClassicConfigFile(true))
	envSecret := os.Getenv("CROC_SECRET")
	crocOptions.SharedSecret = resolveSendSharedSecret(crocOptions.SharedSecret, envSecret)
	if shouldExitForUnixSendCode(runtime.GOOS, c.IsSet("code"), classicInsecureMode, envSecret) {
		fmt.Printf(`On UNIX systems, to send with a custom code phrase,
you need to set the environmental variable CROC_SECRET:

  CROC_SECRET=**** croc send file.txt

Or you can have the code phrase automatically generated:

  croc send file.txt

Or you can go back to the classic croc behavior by enabling classic mode:

  croc --classic

`)
		os.Exit(0)
	}

	generatedPublicCode := len(crocOptions.SharedSecret) == 0 && publicRelayMode
	if len(crocOptions.SharedSecret) == 0 {
		if publicRelayMode {
			var relayIndex int
			relayIndex, err = selectPublicRelay(tcp.MeasureServerLatencyContext)
			if err != nil {
				return err
			}
			crocOptions.SharedSecret, err = codephrase.GenerateForRelay(relayIndex, len(publicrelay.Relays()))
			if err == nil {
				err = assignPublicRelay(&crocOptions, relayIndex)
			}
		} else {
			crocOptions.SharedSecret, err = codephrase.Generate()
		}
		if err != nil {
			return fmt.Errorf("could not generate code phrase: %w", err)
		}
	} else if publicRelayMode {
		if err = assignPublicRelayForCode(&crocOptions); err != nil {
			return fmt.Errorf("could not select public relay: %w", err)
		}
	}
	minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders, err := croc.GetFilesInfoWithExactExclusions(fnames, crocOptions.ZipFolder, crocOptions.GitIgnore, crocOptions.Exclude, crocOptions.ExcludeFile)
	if err != nil {
		return
	}
	if len(crocOptions.Exclude) > 0 {
		minimalFileInfosInclude := []croc.FileInfo{}
		emptyFoldersToTransferInclude := []croc.FileInfo{}
		for _, f := range minimalFileInfos {
			exclude := false
			for _, exclusion := range crocOptions.Exclude {
				if strings.Contains(path.Join(strings.ToLower(f.FolderRemote), strings.ToLower(f.Name)), exclusion) {
					exclude = true
					break
				}
			}
			if !exclude {
				minimalFileInfosInclude = append(minimalFileInfosInclude, f)
			}
		}
		for _, f := range emptyFoldersToTransfer {
			exclude := false
			for _, exclusion := range crocOptions.Exclude {
				if strings.Contains(path.Join(strings.ToLower(f.FolderRemote), strings.ToLower(f.Name)), exclusion) {
					exclude = true
					break
				}
			}
			if !exclude {
				emptyFoldersToTransferInclude = append(emptyFoldersToTransferInclude, f)
			}
		}
		totalNumberFolders = 0
		folderMap := make(map[string]bool)
		for _, f := range minimalFileInfosInclude {
			folderMap[f.FolderRemote] = true
		}
		for _, f := range emptyFoldersToTransferInclude {
			folderMap[f.FolderRemote] = true
		}
		totalNumberFolders = len(folderMap)
		minimalFileInfos = minimalFileInfosInclude
		emptyFoldersToTransfer = emptyFoldersToTransferInclude
	}

	cr, err := croc.New(crocOptions)
	if err != nil {
		return
	}

	// save the config
	saveConfig(c, crocOptions)
	err = cr.Send(minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders)
	clearBestPublicRelayOnSendError(generatedPublicCode, err)
	return
}

func getStdin() (fnames []string, err error) {
	f, err := os.CreateTemp(".", "croc-stdin-")
	if err != nil {
		return
	}
	_, err = io.Copy(f, os.Stdin)
	if err != nil {
		return
	}
	err = f.Close()
	if err != nil {
		return
	}
	fnames = []string{f.Name()}
	return
}

func makeTempFileWithString(s string) (fnames []string, err error) {
	f, err := os.CreateTemp(".", "croc-stdin-")
	if err != nil {
		return
	}

	_, err = f.WriteString(s)
	if err != nil {
		return
	}

	err = f.Close()
	if err != nil {
		return
	}
	fnames = []string{f.Name()}
	return
}

// writePrivateConfigFile writes configuration only after enforcing owner-only
// permissions. os.WriteFile's permission argument applies only to newly created
// files, so it does not harden configs created by older croc versions.
func writePrivateConfigFile(name string, data []byte) (err error) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	if err = f.Chmod(0o600); err != nil {
		return err
	}
	if err = f.Truncate(0); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

func saveConfig(c *cli.Context, crocOptions croc.Options) {
	if c.Bool("remember") {
		configFile := getSendConfigFile(true)
		log.Debug("saving config file")
		var bConfig []byte
		// if the code wasn't set, don't save it
		if c.String("code") == "" {
			crocOptions.SharedSecret = ""
		}
		if c.String("relay") != models.DEFAULT_RELAY {
			crocOptions.RelayAddress = "non-default: " + c.String("relay")
		} else {
			crocOptions.RelayAddress = "default"
		}
		if c.String("relay6") != models.DEFAULT_RELAY6 {
			crocOptions.RelayAddress6 = "non-default: " + c.String("relay6")
		} else {
			crocOptions.RelayAddress6 = "default"
		}
		bConfig, err := json.MarshalIndent(crocOptions, "", "    ")
		if err != nil {
			log.Error(err)
			return
		}
		err = writePrivateConfigFile(configFile, bConfig)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("wrote %s", configFile)
	}
}

func receive(c *cli.Context) (err error) {
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")
	if storedToken := strings.TrimSpace(os.Getenv("CROC_STORE_TOKEN")); storedToken != "" {
		setDebugLevel(c)
		return receiveStored(c, storedToken)
	}
	crocOptions := croc.Options{
		SharedSecret:      c.String("code"),
		IsSender:          false,
		Debug:             c.Bool("debug"),
		NoPrompt:          c.Bool("yes"),
		RelayAddress:      c.String("relay"),
		RelayAddress6:     c.String("relay6"),
		Stdout:            c.Bool("stdout"),
		Ask:               c.Bool("ask"),
		RelayPassword:     determinePass(c),
		OnlyLocal:         c.Bool("local"),
		IP:                c.String("ip"),
		Overwrite:         c.Bool("overwrite"),
		Rename:            c.Bool("rename"),
		Curve:             c.String("curve"),
		TestFlag:          c.Bool("testing"),
		MulticastAddress:  c.String("multicast"),
		Quiet:             c.Bool("quiet"),
		DisableClipboard:  c.Bool("disable-clipboard"),
		ExtendedClipboard: c.Bool("extended-clipboard"),
	}
	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}

	switch c.Args().Len() {
	case 1:
		crocOptions.SharedSecret = c.Args().First()
	case 3:
		fallthrough
	case 4:
		var phrase []string
		phrase = append(phrase, c.Args().First())
		phrase = append(phrase, c.Args().Tail()...)
		crocOptions.SharedSecret = strings.Join(phrase, "-")
	}
	if storeclient.IsStoredValue(crocOptions.SharedSecret) {
		setDebugLevel(c)
		if runtime.GOOS != "windows" && !utils.Exists(getClassicConfigFile(true)) {
			fmt.Print(`For security, stored-transfer links are not accepted as command-line
arguments on UNIX systems because their decryption key would be visible in
the process list.

Run croc with no argument and paste the link at the prompt, or use:

  CROC_STORE_TOKEN='croc-store-v1....' croc

`)
			return nil
		}
		return receiveStored(c, crocOptions.SharedSecret)
	}

	// load options here
	setDebugLevel(c)

	doRemember := c.Bool("remember")
	configFile, err := getReceiveConfigFile(doRemember)
	if err != nil && doRemember {
		return
	}
	b, errOpen := os.ReadFile(configFile)
	if errOpen == nil && !doRemember {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly Globally set
		if !c.IsSet("yes") {
			crocOptions.NoPrompt = rememberedOptions.NoPrompt
		}
		if crocOptions.SharedSecret == "" {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
		if !c.IsSet("pass") && rememberedOptions.RelayPassword != "" {
			crocOptions.RelayPassword = rememberedOptions.RelayPassword
		}
		if !c.IsSet("overwrite") {
			crocOptions.Overwrite = rememberedOptions.Overwrite
		}
		if !c.IsSet("rename") {
			crocOptions.Rename = rememberedOptions.Rename
		}
		if !c.IsSet("curve") && rememberedOptions.Curve != "" {
			crocOptions.Curve = rememberedOptions.Curve
		}
		if !c.IsSet("local") {
			crocOptions.OnlyLocal = rememberedOptions.OnlyLocal
		}
		if !c.IsSet("relay") && strings.HasPrefix(rememberedOptions.RelayAddress, "non-default:") {
			var rememberedAddr = strings.TrimPrefix(rememberedOptions.RelayAddress, "non-default:")
			rememberedAddr = strings.TrimSpace(rememberedAddr)
			crocOptions.RelayAddress = rememberedAddr
		}
		if !c.IsSet("relay6") && strings.HasPrefix(rememberedOptions.RelayAddress6, "non-default:") {
			var rememberedAddr = strings.TrimPrefix(rememberedOptions.RelayAddress6, "non-default:")
			rememberedAddr = strings.TrimSpace(rememberedAddr)
			crocOptions.RelayAddress6 = rememberedAddr
		}
	}
	publicRelayMode := usesPublicRelay(c, crocOptions)

	classicInsecureMode := utils.Exists(getClassicConfigFile(true))
	if crocOptions.SharedSecret == "" && os.Getenv("CROC_SECRET") != "" {
		crocOptions.SharedSecret = os.Getenv("CROC_SECRET")
	} else if !(runtime.GOOS == "windows") && crocOptions.SharedSecret != "" && !classicInsecureMode {
		crocOptions.SharedSecret = os.Getenv("CROC_SECRET")
		if crocOptions.SharedSecret == "" {
			fmt.Printf(`On UNIX systems, to receive with croc you either need
to set a code phrase using your environmental variables:

  CROC_SECRET=**** croc

Or you can specify the code phrase when you run croc without
declaring the secret on the command line:

  croc
  Enter receive code: ****

Or you can go back to the classic croc behavior by enabling classic mode:

  croc --classic

`)
			os.Exit(0)
		}
	}
	if crocOptions.SharedSecret == "" {
		crocOptions.SharedSecret, err = utils.GetInput("Enter receive code: ")
		if err != nil {
			return fmt.Errorf("could not read receive code: %w", err)
		}
	}
	if storeclient.IsStoredValue(crocOptions.SharedSecret) {
		return receiveStored(c, crocOptions.SharedSecret)
	}
	if publicRelayMode {
		if err = assignPublicRelayForCode(&crocOptions); err != nil {
			return fmt.Errorf("could not select public relay: %w", err)
		}
	}
	if c.String("out") != "" {
		if err = os.Chdir(c.String("out")); err != nil {
			return err
		}
	}

	cr, err := croc.New(crocOptions)
	if err != nil {
		return
	}

	// save the config
	if doRemember {
		log.Debug("saving config file")
		var bConfig []byte
		if c.String("relay") != models.DEFAULT_RELAY {
			crocOptions.RelayAddress = "non-default: " + c.String("relay")
		} else {
			crocOptions.RelayAddress = "default"
		}
		if c.String("relay6") != models.DEFAULT_RELAY6 {
			crocOptions.RelayAddress6 = "non-default: " + c.String("relay6")
		} else {
			crocOptions.RelayAddress6 = "default"
		}
		bConfig, err = json.MarshalIndent(crocOptions, "", "    ")
		if err != nil {
			log.Error(err)
			return
		}
		err = writePrivateConfigFile(configFile, bConfig)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("wrote %s", configFile)
	}

	err = cr.Receive()
	return
}

func relay(c *cli.Context) (err error) {
	log.Infof("starting croc relay version %v", Version)
	maxRoomsOpen := c.Int("max-rooms-open")
	if maxRoomsOpen <= 0 {
		return fmt.Errorf("--max-rooms-open must be positive")
	}
	maxPendingHandshakes := c.Int("max-pending-handshakes")
	if maxPendingHandshakes <= 0 {
		return fmt.Errorf("--max-pending-handshakes must be positive")
	}
	handshakeTimeout := c.Duration("handshake-timeout")
	if handshakeTimeout <= 0 {
		return fmt.Errorf("--handshake-timeout must be positive")
	}
	debugString := "info"
	if c.Bool("debug") {
		debugString = "debug"
	}
	host := c.String("host")
	var ports []string

	if c.IsSet("ports") {
		ports = parseRelayPorts(c.String("ports"))
	} else {
		portString := c.Int("port")
		if portString == 0 {
			portString = 9009
		}
		transfersString := c.Int("transfers")
		if transfersString == 0 {
			transfersString = 4
		}
		ports = make([]string, transfersString)
		for i := range ports {
			ports[i] = strconv.Itoa(portString + i)
		}
	}
	if len(ports) < 2 {
		return fmt.Errorf("relay requires at least two ports; specify --ports with two or more ports or set --transfers to 2+")
	}

	var roomPaired func()
	umamiURL := strings.TrimSpace(os.Getenv("UMAMI_URL"))
	umamiWebsiteID := strings.TrimSpace(os.Getenv("UMAMI_WEBSITE_ID"))
	siteURL := strings.TrimSpace(os.Getenv("SITE_URL"))
	if umamiURL != "" && umamiWebsiteID != "" && siteURL != "" {
		reporter, reporterErr := publicrelay.NewUmamiReporter(umamiURL, umamiWebsiteID, siteURL, Version)
		if reporterErr != nil {
			log.Warnf("relay analytics disabled: %v", reporterErr)
		} else {
			defer reporter.Close()
			roomPaired = func() {
				reporter.Track("relay-session")
			}
		}
	}

	tcpPorts := strings.Join(ports[1:], ",")
	for i, port := range ports {
		if i == 0 {
			continue
		}
		go func(portStr string) {
			err := tcp.RunWithOptionsAsync(
				host,
				portStr,
				determinePass(c),
				tcp.WithLogLevel(debugString),
				tcp.WithMaxRoomsOpen(maxRoomsOpen),
				tcp.WithMaxPendingHandshakes(maxPendingHandshakes),
				tcp.WithHandshakeTimeout(handshakeTimeout),
			)
			if err != nil {
				panic(err)
			}
		}(port)
	}
	return tcp.RunWithOptionsAsync(
		host,
		ports[0],
		determinePass(c),
		tcp.WithBanner(tcpPorts),
		tcp.WithLogLevel(debugString),
		tcp.WithMaxRoomsOpen(maxRoomsOpen),
		tcp.WithMaxPendingHandshakes(maxPendingHandshakes),
		tcp.WithHandshakeTimeout(handshakeTimeout),
		tcp.WithRoomPairedCallback(roomPaired),
	)
}
