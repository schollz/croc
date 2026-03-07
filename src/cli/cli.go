package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/schollz/cli/v2"
	"github.com/schollz/croc/v10/src/comm"
	"github.com/schollz/croc/v10/src/croc"
	"github.com/schollz/croc/v10/src/mnemonicode"
	"github.com/schollz/croc/v10/src/models"
	poollib "github.com/schollz/croc/v10/src/pool"
	"github.com/schollz/croc/v10/src/tcp"
	"github.com/schollz/croc/v10/src/utils"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"
)

// Version specifies the version
var Version string

// Run will run the command line program
func Run() (err error) {
	// use all of the processors
	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "croc"
	if Version == "" {
		Version = "v10.4.1"
	}
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
				&cli.StringFlag{Name: "code", Aliases: []string{"c"}, Usage: "codephrase used to connect to relay"},
				&cli.StringFlag{Name: "hash", Value: "xxhash", Usage: "hash algorithm (xxhash, imohash, md5)"},
				&cli.StringFlag{Name: "text", Aliases: []string{"t"}, Usage: "send some text"},
				&cli.BoolFlag{Name: "no-local", Usage: "disable local relay when sending"},
				&cli.BoolFlag{Name: "no-multi", Usage: "disable multiplexing"},
				&cli.BoolFlag{Name: "git", Usage: "enable .gitignore respect / don't send ignored files"},
				&cli.IntFlag{Name: "port", Value: 9009, Usage: "base port for the relay"},
				&cli.IntFlag{Name: "transfers", Value: 4, Usage: "number of ports to use for transfers"},
				&cli.BoolFlag{Name: "qrcode", Aliases: []string{"qr"}, Usage: "show receive code as a qrcode"},
				&cli.StringFlag{Name: "exclude", Value: "", Usage: "exclude files if they contain any of the comma separated strings"},
				&cli.StringFlag{Name: "socks5", Value: "", Usage: "add a socks5 proxy", EnvVars: []string{"SOCKS5_PROXY"}},
				&cli.StringFlag{Name: "connect", Value: "", Usage: "add a http proxy", EnvVars: []string{"HTTP_PROXY"}},
			},
			HelpName: "croc send",
			Action:   send,
		},
		{
			Name:  "relay",
			Usage: "start a croc relay",
			Description: `Start a relay in one of three modes:

	main:      Start a main relay with integrated pool API (public pool coordinator)
	community: Start a community relay that registers with a pool API (public volunteer relay)
   private:   Start a private relay (not publicly registered, default)

EXAMPLES:
	Start main relay with pool API:
		croc relay --role main --host 0.0.0.0 --ports 9009,9010,9011,9012,9013 --pool-port 8080
   
   Start community relay:
		croc relay --role community --host 0.0.0.0 --pool http://pool.example.com:8080
   
   Start private relay:
      croc relay --host 0.0.0.0
   
	Short flag alias:
		croc relay --rol main --host 0.0.0.0`,
			ArgsUsage: "",
			HelpName:  "croc relay",
			Action:    relay,
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "role", Aliases: []string{"rol"}, Usage: "relay role: main, community, or private", EnvVars: []string{"CROC_RELAY_ROLE"}},
				&cli.StringFlag{Name: "host", Value: "0.0.0.0", Usage: "host address to bind to (default: 0.0.0.0 for all IPv4 interfaces)"},
				&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "comma-separated list of ports for the relay"},
				&cli.IntFlag{Name: "port", Value: 9009, Usage: "base port for the relay (used with --transfers)"},
				&cli.IntFlag{Name: "transfers", Value: 5, Usage: "number of ports to use for relay (starting from --port)"},
				&cli.IntFlag{Name: "pool-port", Value: 8080, Usage: "port for integrated pool HTTP server (only for 'main' role)"},
				&cli.StringFlag{Name: "pool", Value: models.DEFAULT_POOL, Usage: "pool API URL for relay registration (for 'main' and 'community' roles)", EnvVars: []string{"CROC_POOL"}},
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
		&cli.BoolFlag{Name: "testing", Usage: "flag for testing purposes"},
		&cli.BoolFlag{Name: "quiet", Usage: "disable all output"},
		&cli.BoolFlag{Name: "disable-clipboard", Usage: "disable copy to clipboard"},
		&cli.BoolFlag{Name: "extended-clipboard", Usage: "copy full command with secret as env variable to clipboard"},
		&cli.StringFlag{Name: "multicast", Value: "239.255.255.250", Usage: "multicast address to use for local discovery"},
		&cli.StringFlag{Name: "curve", Value: "p256", Usage: "choose an encryption curve (" + strings.Join(pake.AvailableCurves(), ", ") + ")"},
		&cli.StringFlag{Name: "ip", Value: "", Usage: "set sender ip if known e.g. 10.0.0.1:9009, [::1]:9009"},
		&cli.StringFlag{Name: "relay", Value: models.DEFAULT_RELAY, Usage: "address of the relay", EnvVars: []string{"CROC_RELAY"}},
		&cli.StringFlag{Name: "relay6", Value: models.DEFAULT_RELAY6, Usage: "ipv6 address of the relay", EnvVars: []string{"CROC_RELAY6"}},
		&cli.StringFlag{Name: "pool", Value: models.DEFAULT_POOL, Usage: "address of the relay pool API", EnvVars: []string{"CROC_POOL"}},
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
				choice := strings.ToLower(utils.GetInput(""))
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
				choice := strings.ToLower(utils.GetInput(""))
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
			choice := strings.ToLower(utils.GetInput(promptMessage))
			if choice == "" || choice == "y" || choice == "yes" {
				return send(c)
			}
		}

		return receive(c)
	}

	return app.Run(os.Args)
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
		pass = strings.TrimSpace(string(b))
	}
	return
}

func localIPv4Set() map[string]struct{} {
	result := make(map[string]struct{})
	interfaces, err := net.Interfaces()
	if err != nil {
		return result
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				result[v4.String()] = struct{}{}
			}
		}
	}

	return result
}

// detectUsablePublicIPv4 verifies that we have a real public IPv4 bound to a local interface.
// If the detected public IPv4 is not local, we are likely behind NAT and cannot act as a public relay.
func detectUsablePublicIPv4() (string, error) {
	publicIP, err := utils.PublicIP()
	if err != nil {
		return "", fmt.Errorf("could not detect public IPv4: %w", err)
	}

	parsed := net.ParseIP(publicIP)
	if parsed == nil || parsed.To4() == nil || !utils.IsPublicIP(publicIP) {
		return "", fmt.Errorf("detected IP %q is not a usable public IPv4", publicIP)
	}

	localIPs := localIPv4Set()
	if _, ok := localIPs[publicIP]; !ok {
		return "", fmt.Errorf("detected public IPv4 %s is not bound to this host (likely behind NAT)", publicIP)
	}

	return publicIP, nil
}

func waitForPoolReady(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("pool API did not become ready at %s within %s", url, timeout)
}

func relayRoleFromInvocation(c *cli.Context) (role string, err error) {
	if c.Args().Len() > 0 {
		arg := strings.TrimSpace(c.Args().First())
		if arg != "" {
			return "", fmt.Errorf("positional relay role is no longer supported: %q\nuse --role (or --rol), e.g. 'croc relay --role main'", arg)
		}
	}

	// Priority order: --role flag > environment variable > default (private)
	role = strings.TrimSpace(c.String("role"))
	if role == "" {
		// Finally check environment variable
		role = strings.TrimSpace(os.Getenv("CROC_RELAY_ROLE"))
	}
	if role == "" {
		// Default to private relay (not publicly registered)
		return "private", nil
	}

	// Validate the role
	switch role {
	case "main", "community", "private":
		return role, nil
	default:
		return "", fmt.Errorf("invalid relay role %q\n\nAllowed roles:\n  main      - start main relay with integrated pool API\n  community - start community relay (registers with pool API)\n  private   - start private relay (not publicly registered)\n\nUsage: croc relay --role [role] [options]", role)
	}
}

func send(c *cli.Context) (err error) {
	setDebugLevel(c)
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")

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
		PoolURL:           c.String("pool"),
		SendingText:       c.String("text") != "",
		NoCompress:        c.Bool("no-compress"),
		Overwrite:         c.Bool("overwrite"),
		Curve:             c.String("curve"),
		HashAlgorithm:     c.String("hash"),
		ThrottleUpload:    c.String("throttleUpload"),
		ZipFolder:         c.Bool("zip"),
		GitIgnore:         c.Bool("git"),
		ShowQrCode:        c.Bool("qrcode"),
		MulticastAddress:  c.String("multicast"),
		Exclude:           excludeStrings,
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
		// update anything that isn't explicitly set
		if !c.IsSet("no-local") {
			crocOptions.DisableLocal = rememberedOptions.DisableLocal
		}
		if !c.IsSet("ports") && len(rememberedOptions.RelayPorts) > 0 {
			crocOptions.RelayPorts = rememberedOptions.RelayPorts
		}
		if !c.IsSet("code") {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
		if !c.IsSet("pass") && rememberedOptions.RelayPassword != "" {
			crocOptions.RelayPassword = rememberedOptions.RelayPassword
		}
		if !c.IsSet("overwrite") {
			crocOptions.Overwrite = rememberedOptions.Overwrite
		}
		if !c.IsSet("curve") && rememberedOptions.Curve != "" {
			crocOptions.Curve = rememberedOptions.Curve
		}
		if !c.IsSet("local") {
			crocOptions.OnlyLocal = rememberedOptions.OnlyLocal
		}
		if !c.IsSet("hash") {
			crocOptions.HashAlgorithm = rememberedOptions.HashAlgorithm
		}
		if !c.IsSet("git") {
			crocOptions.GitIgnore = rememberedOptions.GitIgnore
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
	if !classicInsecureMode {
		// if operating system is UNIX, then use environmental variable to set the code
		if (!(runtime.GOOS == "windows") && c.IsSet("code")) || os.Getenv("CROC_SECRET") != "" {
			crocOptions.SharedSecret = os.Getenv("CROC_SECRET")
			if crocOptions.SharedSecret == "" {
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
		}
	}

	if len(crocOptions.SharedSecret) == 0 {
		// generate code phrase
		crocOptions.SharedSecret = utils.GetRandomName()
	}
	minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders, err := croc.GetFilesInfo(fnames, crocOptions.ZipFolder, crocOptions.GitIgnore, crocOptions.Exclude)
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
		err = os.WriteFile(configFile, bConfig, 0o644)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("wrote %s", configFile)
	}
}

type TabComplete struct{}

func (t TabComplete) Do(line []rune, pos int) ([][]rune, int) {
	var words = strings.SplitAfter(string(line), "-")
	var lastPartialWord = words[len(words)-1]
	var nbCharacter = len(lastPartialWord)
	if nbCharacter == 0 {
		// No completion
		return [][]rune{[]rune("")}, 0
	}
	if len(words) == 1 && nbCharacter == utils.NbPinNumbers {
		// Check if word is indeed a number
		_, err := strconv.Atoi(lastPartialWord)
		if err == nil {
			return [][]rune{[]rune("-")}, nbCharacter
		}
	}
	var strArray [][]rune
	for _, s := range mnemonicode.WordList {
		if strings.HasPrefix(s, lastPartialWord) {
			var completionCandidate = s[nbCharacter:]
			if len(words) <= mnemonicode.WordsRequired(utils.NbBytesWords) {
				completionCandidate += "-"
			}
			strArray = append(strArray, []rune(completionCandidate))
		}
	}
	return strArray, nbCharacter
}

func receive(c *cli.Context) (err error) {
	comm.Socks5Proxy = c.String("socks5")
	comm.HttpProxy = c.String("connect")
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
		PoolURL:           c.String("pool"),
		OnlyLocal:         c.Bool("local"),
		IP:                c.String("ip"),
		Overwrite:         c.Bool("overwrite"),
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
		l, err := readline.NewEx(&readline.Config{
			Prompt:       "Enter receive code: ",
			AutoComplete: TabComplete{},
		})
		if err != nil {
			return err
		}
		crocOptions.SharedSecret, err = l.Readline()
		if err != nil {
			return err
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
		err = os.WriteFile(configFile, bConfig, 0o644)
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
	debugString := "info"
	if c.Bool("debug") {
		debugString = "debug"
	}

	relayRole, err := relayRoleFromInvocation(c)
	if err != nil {
		return err
	}

	publicMode := relayRole != "private"
	withPoolServerMode := relayRole == "main"
	log.Infof("Relay mode: %s (public=%v, poolServer=%v)", relayRole, publicMode, withPoolServerMode)

	host := c.String("host")
	if host == "" {
		host = "0.0.0.0"
	}
	var ports []string
	poolURL := c.String("pool")

	if c.IsSet("ports") {
		ports = strings.Split(c.String("ports"), ",")
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

	if withPoolServerMode {
		publicIPv4, detectErr := detectUsablePublicIPv4()
		if detectErr != nil {
			return fmt.Errorf("main relay requires a public IPv4 directly assigned to this host: %w", detectErr)
		}

		poolPort := c.Int("pool-port")
		if poolPort <= 0 {
			return fmt.Errorf("--pool-port must be greater than 0")
		}

		poolPortStr := strconv.Itoa(poolPort)
		for _, relayPort := range ports {
			if relayPort == poolPortStr {
				return fmt.Errorf("--pool-port %d conflicts with relay port %s", poolPort, relayPort)
			}
		}

		// For main relay, use detected public IPv4 so registration source/claim can match.
		if !c.IsSet("pool") || poolURL == "" {
			poolURL = fmt.Sprintf("http://%s:%d", publicIPv4, poolPort)
		}

		go func() {
			config := poollib.ServerConfig{
				Host:            "0.0.0.0", // IPv4 only binding
				Port:            poolPort,
				TTL:             10 * time.Minute,
				CleanupInterval: 1 * time.Minute,
			}
			log.Infof("Starting integrated pool API on %s:%d", config.Host, config.Port)
			if err := poollib.RunServer(config); err != nil {
				log.Errorf("integrated pool API stopped: %v", err)
			}
		}()

		log.Infof("Waiting for pool API to be ready at %s...", poolURL)
		if err := waitForPoolReady(fmt.Sprintf("http://127.0.0.1:%d", poolPort), 8*time.Second); err != nil {
			return fmt.Errorf("failed to start integrated pool API on IPv4 (0.0.0.0:%d): %w\n\npublic relay mode requires a public IPv4 address that is reachable from the internet", poolPort, err)
		}
		log.Infof("Pool API is ready and accepting registrations")
	}

	// Handle public registration
	var publicOpts []interface{}
	if publicMode {
		if _, detectErr := detectUsablePublicIPv4(); detectErr != nil {
			return fmt.Errorf("public relay mode requires a public IPv4 directly assigned to this host: %w", detectErr)
		}

		// Use DEFAULT_POOL if no pool endpoint was specified and not starting a new pool API
		if poolURL == "" && !withPoolServerMode {
			poolURL = models.DEFAULT_POOL
			log.Infof("No pool endpoint specified; using default pool API: %s", poolURL)
		}
		if poolURL == "" {
			return fmt.Errorf("public relay roles require a pool API URL (use --pool or set CROC_POOL)")
		}

		// Detect public IPv4 address for relay registration
		// Note: IPv6 is not supported by the pool API at this time
		var publicAddresses []string

		// Try IPv4
		ipv4, err4 := utils.PublicIP()
		if err4 == nil {
			addr := fmt.Sprintf("%s:%s", ipv4, ports[0])
			if err := utils.ValidatePublicRelayAddress(addr); err == nil {
				publicAddresses = append(publicAddresses, addr)
				log.Infof("Detected public IPv4 address: %s", addr)
			} else {
				log.Warnf("IPv4 address detected but not valid as public relay: %v", err)
			}
		} else {
			log.Warnf("Failed to detect public IPv4 address: %v", err4)
		}

		// Check for IPv6 and inform user it's not supported
		ipv6, err6 := utils.PublicIPv6()
		if err6 == nil {
			log.Infof("Detected public IPv6 address: [%s]:%s (not registered - pool API only supports IPv4)", ipv6, ports[0])
		}

		if len(publicAddresses) == 0 {
			return fmt.Errorf("failed to detect public IPv4 address for %s relay\\n\\nPlease ensure:\\n  - Your server has a public IPv4 address\\n  - The IPv4 is not behind NAT or firewall blocking external services\\n  - External IP detection services are accessible\\n\\nNote: IPv6-only servers are not currently supported by the pool API", relayRole)
		}

		log.Infof("Public %s relay: registering %d address(es) with pool API at %s", relayRole, len(publicAddresses), poolURL)
		publicOpts = append(publicOpts, tcp.WithPublicRegistration(poolURL, publicAddresses, Version))
	}

	tcpPorts := strings.Join(ports[1:], ",")
	for i, port := range ports {
		if i == 0 {
			continue
		}
		go func(portStr string) {
			// Transfer-port servers must not register independently; only the main
			// relay server should maintain pool registration/token.
			opts := []interface{}{tcp.WithLogLevel(debugString)}
			err := tcp.RunWithOptions(host, portStr, determinePass(c), opts...)
			if err != nil {
				panic(err)
			}
		}(port)
	}

	// Main port with banner
	opts := []interface{}{tcp.WithBanner(tcpPorts), tcp.WithLogLevel(debugString)}
	if len(publicOpts) > 0 {
		opts = append(opts, publicOpts...)
	}
	return tcp.RunWithOptions(host, ports[0], determinePass(c), opts...)
}
