package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/schollz/croc/v8/src/croc"
	"github.com/schollz/croc/v8/src/models"
	"github.com/schollz/croc/v8/src/tcp"
	"github.com/schollz/croc/v8/src/utils"
	log "github.com/schollz/logger"
	"github.com/urfave/cli/v2"
)

// Version specifies the version
var Version string

// Run will run the command line proram
func Run() (err error) {
	// use all of the processors
	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "croc"
	if Version == "" {
		Version = "v8.3.1-9d5302b"
	}
	app.Version = Version
	app.Compiled = time.Now()
	app.Usage = "easily and securely transfer stuff from one computer to another"
	app.UsageText = `Send a file:
      croc send file.txt

   Send a file with a custom code:
      croc send --code secret-passphrase file.txt`
	app.Commands = []*cli.Command{
		{
			Name:        "send",
			Usage:       "send a file (see options with croc send -h)",
			Description: "send a file over the relay",
			ArgsUsage:   "[filename]",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "code, c", Usage: "codephrase used to connect to relay"},
				&cli.StringFlag{Name: "text, t", Usage: "send some text"},
				&cli.BoolFlag{Name: "no-local", Usage: "disable local relay when sending"},
				&cli.BoolFlag{Name: "no-multi", Usage: "disable multiplexing"},
				&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the local relay (optional)"},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return send(c)
			},
		},
		{
			Name:        "relay",
			Usage:       "start your own relay (optional)",
			Description: "start relay",
			HelpName:    "croc relay",
			Action: func(c *cli.Context) error {
				return relay(c)
			},
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the relay"},
			},
		},
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{Name: "remember", Usage: "save these settings to reuse next time"},
		&cli.BoolFlag{Name: "debug", Usage: "toggle debug mode"},
		&cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		&cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		&cli.BoolFlag{Name: "no-compress", Usage: "disable compression"},
		&cli.BoolFlag{Name: "ask", Usage: "make sure sender and recipient are prompted"},
		&cli.StringFlag{Name: "relay", Value: models.DEFAULT_RELAY, Usage: "address of the relay"},
		&cli.StringFlag{Name: "relay6", Value: models.DEFAULT_RELAY6, Usage: "ipv6 address of the relay"},
		&cli.StringFlag{Name: "out", Value: ".", Usage: "specify an output folder to receive the file"},
		&cli.StringFlag{Name: "pass", Value: "pass123", Usage: "password for the relay"},
	}
	app.EnableBashCompletion = true
	app.HideHelp = false
	app.HideVersion = false
	app.BashComplete = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "send\nreceive\relay")
	}
	app.Action = func(c *cli.Context) error {
		// if trying to send but forgot send, let the user know
		if c.Args().First() != "" && utils.Exists(c.Args().First()) {
			_, fname := filepath.Split(c.Args().First())
			yn := utils.GetInput(fmt.Sprintf("Did you mean to send '%s'? (y/n) ", fname))
			if strings.ToLower(yn) == "y" {
				return send(c)
			}
		}
		return receive(c)
	}

	return app.Run(os.Args)
}

func getConfigDir() (homedir string, err error) {
	homedir, err = os.UserHomeDir()
	if err != nil {
		log.Error(err)
		return
	}
	homedir = path.Join(homedir, ".config", "croc")
	if _, err = os.Stat(homedir); os.IsNotExist(err) {
		log.Debugf("creating home directory %s", homedir)
		err = os.MkdirAll(homedir, 0700)
	}
	return
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
	configFile, err := getConfigDir()
	if err != nil {
		log.Error(err)
		return ""
	}
	return path.Join(configFile, "send.json")
}

func determinePass(c *cli.Context) (pass string) {
	pass = c.String("pass")
	b, err := ioutil.ReadFile(pass)
	if err == nil {
		pass = strings.TrimSpace(string(b))
	}
	return
}

func send(c *cli.Context) (err error) {
	setDebugLevel(c)
	crocOptions := croc.Options{
		SharedSecret:   c.String("code"),
		IsSender:       true,
		Debug:          c.Bool("debug"),
		NoPrompt:       c.Bool("yes"),
		RelayAddress:   c.String("relay"),
		RelayAddress6:  c.String("relay6"),
		Stdout:         c.Bool("stdout"),
		DisableLocal:   c.Bool("no-local"),
		RelayPorts:     strings.Split(c.String("ports"), ","),
		Ask:            c.Bool("ask"),
		NoMultiplexing: c.Bool("no-multi"),
		RelayPassword:  determinePass(c),
		SendingText:    c.String("text") != "",
		NoCompress:     c.Bool("no-compress"),
	}
	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}
	b, errOpen := ioutil.ReadFile(getConfigFile())
	if errOpen == nil && !c.Bool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly set
		if !c.IsSet("relay") {
			crocOptions.RelayAddress = rememberedOptions.RelayAddress
		}
		if !c.IsSet("no-local") {
			crocOptions.DisableLocal = rememberedOptions.DisableLocal
		}
		if !c.IsSet("ports") {
			crocOptions.RelayPorts = rememberedOptions.RelayPorts
		}
		if !c.IsSet("code") {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
		if !c.IsSet("pass") {
			crocOptions.RelayPassword = rememberedOptions.RelayPassword
		}
	}

	var fnames []string
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		fnames, err = getStdin()
		if err != nil {
			return
		}
		defer func() {
			err = os.Remove(fnames[0])
			if err != nil {
				log.Error(err)
			}
		}()
	} else if c.String("text") != "" {
		fnames, err = makeTempFileWithString(c.String("text"))
		if err != nil {
			return
		}
		defer func() {
			err = os.Remove(fnames[0])
			if err != nil {
				log.Error(err)
			}
		}()

	} else {
		fnames = append([]string{c.Args().First()}, c.Args().Tail()...)
	}
	if len(fnames) == 0 {
		return errors.New("must specify file: croc send [filename]")
	}

	if len(crocOptions.SharedSecret) == 0 {
		// generate code phrase
		crocOptions.SharedSecret = utils.GetRandomName()
	}

	paths, haveFolder, err := getPaths(fnames)
	if err != nil {
		return
	}

	cr, err := croc.New(crocOptions)
	if err != nil {
		return
	}

	// save the config
	saveConfig(c, crocOptions)

	err = cr.Send(croc.TransferOptions{
		PathToFiles:      paths,
		KeepPathInRemote: haveFolder,
	})

	return
}

func getStdin() (fnames []string, err error) {
	f, err := ioutil.TempFile(".", "croc-stdin-")
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
	f, err := ioutil.TempFile(".", "croc-stdin-")
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

func getPaths(fnames []string) (paths []string, haveFolder bool, err error) {
	haveFolder = false
	paths = []string{}
	for _, fname := range fnames {
		stat, errStat := os.Stat(fname)
		if errStat != nil {
			err = errStat
			return
		}
		if stat.IsDir() {
			haveFolder = true
			err = filepath.Walk(fname,
				func(pathName string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if !info.IsDir() {
						paths = append(paths, filepath.ToSlash(pathName))
					}
					return nil
				})
			if err != nil {
				return
			}
		} else {
			paths = append(paths, filepath.ToSlash(fname))
		}
	}
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
		err = ioutil.WriteFile(configFile, bConfig, 0644)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("wrote %s", configFile)
	}
}

func receive(c *cli.Context) (err error) {
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
	configFile, err := getConfigDir()
	if err != nil {
		log.Error(err)
		return
	}
	configFile = path.Join(configFile, "receive.json")
	b, errOpen := ioutil.ReadFile(configFile)
	if errOpen == nil && !c.Bool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't expliciGlobalIsSettly set
		if !c.IsSet("relay") {
			crocOptions.RelayAddress = rememberedOptions.RelayAddress
		}
		if !c.IsSet("yes") {
			crocOptions.NoPrompt = rememberedOptions.NoPrompt
		}
		if crocOptions.SharedSecret == "" {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
		if !c.IsSet("pass") {
			crocOptions.RelayPassword = rememberedOptions.RelayPassword
		}
	}

	if crocOptions.SharedSecret == "" {
		crocOptions.SharedSecret = utils.GetInput("Enter receive code: ")
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
		err = ioutil.WriteFile(configFile, bConfig, 0644)
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
	ports := strings.Split(c.String("ports"), ",")
	tcpPorts := strings.Join(ports[1:], ",")
	for i, port := range ports {
		if i == 0 {
			continue
		}
		go func(portStr string) {
			err = tcp.Run(debugString, portStr, determinePass(c))
			if err != nil {
				panic(err)
			}
		}(port)
	}
	return tcp.Run(debugString, ports[0], determinePass(c), tcpPorts)
}
