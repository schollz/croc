package cli

import (
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

	"github.com/chzyer/readline"
	"github.com/schollz/cli/v2"
	"github.com/schollz/croc/v9/src/comm"
	"github.com/schollz/croc/v9/src/croc"
	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/tcp"
	"github.com/schollz/croc/v9/src/utils"
	log "github.com/schollz/logger"
	"github.com/schollz/mnemonicode"
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
		Version = "v9.6.11"
	}
	app.Version = Version
	app.Compiled = time.Now()
	app.Usage = "easily and securely transfer stuff from one computer to another"
	app.UsageText = `Send a file:
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
				&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the relay"},
			},
		},
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{Name: "internal-dns", Usage: "use a built-in DNS stub resolver rather than the host operating system"},
		&cli.BoolFlag{Name: "remember", Usage: "save these settings to reuse next time"},
		&cli.BoolFlag{Name: "debug", Usage: "toggle debug mode"},
		&cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		&cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		&cli.BoolFlag{Name: "no-compress", Usage: "disable compression"},
		&cli.BoolFlag{Name: "ask", Usage: "make sure sender and recipient are prompted"},
		&cli.BoolFlag{Name: "local", Usage: "force to use only local connections"},
		&cli.BoolFlag{Name: "ignore-stdin", Usage: "ignore piped stdin"},
		&cli.BoolFlag{Name: "overwrite", Usage: "do not prompt to overwrite"},
		&cli.BoolFlag{Name: "testing", Usage: "flag for testing purposes"},
		&cli.StringFlag{Name: "curve", Value: "p256", Usage: "choose an encryption curve (" + strings.Join(pake.AvailableCurves(), ", ") + ")"},
		&cli.StringFlag{Name: "ip", Value: "", Usage: "set sender ip if known e.g. 10.0.0.1:9009, [::1]:9009"},
		&cli.StringFlag{Name: "relay", Value: models.DEFAULT_RELAY, Usage: "address of the relay", EnvVars: []string{"CROC_RELAY"}},
		&cli.StringFlag{Name: "relay6", Value: models.DEFAULT_RELAY6, Usage: "ipv6 address of the relay", EnvVars: []string{"CROC_RELAY6"}},
		&cli.StringFlag{Name: "out", Value: ".", Usage: "specify an output folder to receive the file"},
		&cli.StringFlag{Name: "pass", Value: models.DEFAULT_PASSPHRASE, Usage: "password for the relay", EnvVars: []string{"CROC_PASS"}},
		&cli.StringFlag{Name: "socks5", Value: "", Usage: "add a socks5 proxy", EnvVars: []string{"SOCKS5_PROXY"}},
		&cli.StringFlag{Name: "connect", Value: "", Usage: "add a http proxy", EnvVars: []string{"HTTP_PROXY"}},
		&cli.StringFlag{Name: "throttleUpload", Value: "", Usage: "Throttle the upload speed e.g. 500k"},
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
	if c.Bool("debug") {
		log.SetLevel("debug")
		log.Debug("debug mode on")
	} else {
		log.SetLevel("info")
	}
}

func getConfigFile() string {
	configFile, err := utils.GetConfigDir()
	if err != nil {
		log.Error(err)
		return ""
	}
	return path.Join(configFile, "send.json")
}

func determinePass(c *cli.Context) (pass string) {
	pass = c.String("pass")
	b, err := os.ReadFile(pass)
	if err == nil {
		pass = strings.TrimSpace(string(b))
	}
	return
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

	ports := make([]string, transfersParam+1)
	for i := 0; i <= transfersParam; i++ {
		ports[i] = strconv.Itoa(portParam + i)
	}

	crocOptions := croc.Options{
		SharedSecret:   c.String("code"),
		IsSender:       true,
		Debug:          c.Bool("debug"),
		NoPrompt:       c.Bool("yes"),
		RelayAddress:   c.String("relay"),
		RelayAddress6:  c.String("relay6"),
		Stdout:         c.Bool("stdout"),
		DisableLocal:   c.Bool("no-local"),
		OnlyLocal:      c.Bool("local"),
		IgnoreStdin:    c.Bool("ignore-stdin"),
		RelayPorts:     ports,
		Ask:            c.Bool("ask"),
		NoMultiplexing: c.Bool("no-multi"),
		RelayPassword:  determinePass(c),
		SendingText:    c.String("text") != "",
		NoCompress:     c.Bool("no-compress"),
		Overwrite:      c.Bool("overwrite"),
		Curve:          c.String("curve"),
		HashAlgorithm:  c.String("hash"),
		ThrottleUpload: c.String("throttleUpload"),
		ZipFolder:      c.Bool("zip"),
		GitIgnore:      c.Bool("git"),
	}
	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}
	b, errOpen := os.ReadFile(getConfigFile())
	if errOpen == nil && !c.Bool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly set
		if !c.IsSet("relay") && rememberedOptions.RelayAddress != "" {
			crocOptions.RelayAddress = rememberedOptions.RelayAddress
		}
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
		if !c.IsSet("relay6") && rememberedOptions.RelayAddress6 != "" {
			crocOptions.RelayAddress6 = rememberedOptions.RelayAddress6
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
	}

	var fnames []string
	stat, _ := os.Stdin.Stat()
	if ((stat.Mode() & os.ModeCharDevice) == 0) && !c.Bool("ignore-stdin") {
		fnames, err = getStdin()
		if err != nil {
			return
		}
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

	if len(crocOptions.SharedSecret) == 0 {
		// generate code phrase
		crocOptions.SharedSecret = utils.GetRandomName()
	}
	minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders, err := croc.GetFilesInfo(fnames, crocOptions.ZipFolder, crocOptions.GitIgnore)
	if err != nil {
		return
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
		configFile := getConfigFile()
		log.Debug("saving config file")
		var bConfig []byte
		// if the code wasn't set, don't save it
		if c.String("code") == "" {
			crocOptions.SharedSecret = ""
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
		SharedSecret:  c.String("code"),
		IsSender:      false,
		Debug:         c.Bool("debug"),
		NoPrompt:      c.Bool("yes"),
		RelayAddress:  c.String("relay"),
		RelayAddress6: c.String("relay6"),
		Stdout:        c.Bool("stdout"),
		Ask:           c.Bool("ask"),
		RelayPassword: determinePass(c),
		OnlyLocal:     c.Bool("local"),
		IP:            c.String("ip"),
		Overwrite:     c.Bool("overwrite"),
		Curve:         c.String("curve"),
		TestFlag:      c.Bool("testing"),
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
		var phrase []string
		phrase = append(phrase, c.Args().First())
		phrase = append(phrase, c.Args().Tail()...)
		crocOptions.SharedSecret = strings.Join(phrase, "-")
	}

	// load options here
	setDebugLevel(c)
	configFile, err := utils.GetConfigDir()
	if err != nil {
		log.Error(err)
		return
	}
	configFile = path.Join(configFile, "receive.json")
	b, errOpen := os.ReadFile(configFile)
	if errOpen == nil && !c.Bool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly Globally set
		if !c.IsSet("relay") && rememberedOptions.RelayAddress != "" {
			crocOptions.RelayAddress = rememberedOptions.RelayAddress
		}
		if !c.IsSet("yes") {
			crocOptions.NoPrompt = rememberedOptions.NoPrompt
		}
		if crocOptions.SharedSecret == "" {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
		if !c.IsSet("pass") && rememberedOptions.RelayPassword != "" {
			crocOptions.RelayPassword = rememberedOptions.RelayPassword
		}
		if !c.IsSet("relay6") && rememberedOptions.RelayAddress6 != "" {
			crocOptions.RelayAddress6 = rememberedOptions.RelayAddress6
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
	if c.Bool("remember") {
		log.Debug("saving config file")
		var bConfig []byte
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
	host := c.String("host")
	ports := strings.Split(c.String("ports"), ",")
	tcpPorts := strings.Join(ports[1:], ",")
	for i, port := range ports {
		if i == 0 {
			continue
		}
		go func(portStr string) {
			err := tcp.Run(debugString, host, portStr, determinePass(c))
			if err != nil {
				panic(err)
			}
		}(port)
	}
	return tcp.Run(debugString, host, ports[0], determinePass(c), tcpPorts)
}
