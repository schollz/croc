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

	"github.com/schollz/croc/v6/src/croc"
	"github.com/schollz/croc/v6/src/models"
	"github.com/schollz/croc/v6/src/tcp"
	"github.com/schollz/croc/v6/src/utils"
	log "github.com/schollz/logger"
	"github.com/urfave/cli"
)

var Version string

func Run() (err error) {
	// use all of the processors
	runtime.GOMAXPROCS(runtime.NumCPU())

	app := cli.NewApp()
	app.Name = "croc"
	if Version == "" {
		Version = "v6.1.3-bc6803e"
	}
	app.Version = Version
	app.Compiled = time.Now()
	app.Usage = "easily and securely transfer stuff from one computer to another"
	app.UsageText = "croc allows any two computers to directly and securely transfer files"
	// app.ArgsUsage = "[args and such]"
	app.Commands = []cli.Command{
		{
			Name:        "send",
			Usage:       "send a file",
			Description: "send a file over the relay",
			ArgsUsage:   "[filename]",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "code, c", Usage: "codephrase used to connect to relay"},
				cli.BoolFlag{Name: "no-local", Usage: "disable local relay when sending"},
				cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the local relay (optional)"},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return send(c)
			},
		},
		{
			Name:        "relay",
			Description: "start relay",
			HelpName:    "croc relay",
			Action: func(c *cli.Context) error {
				return relay(c)
			},
			Flags: []cli.Flag{
				cli.StringFlag{Name: "ports", Value: "9009,9010,9011,9012,9013", Usage: "ports of the relay"},
			},
		},
	}
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "remember", Usage: "save these settings to reuse next time"},
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity (a lot)"},
		cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		cli.StringFlag{Name: "relay", Value: models.DEFAULT_RELAY, Usage: "address of the relay"},
		cli.StringFlag{Name: "out", Value: ".", Usage: "specify an output folder to receive the file"},
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
	if _, err := os.Stat(homedir); os.IsNotExist(err) {
		log.Debugf("creating home directory %s", homedir)
		err = os.MkdirAll(homedir, 0700)
	}
	return
}

func send(c *cli.Context) (err error) {
	if c.GlobalBool("debug") {
		log.SetLevel("debug")
		log.Debug("debug mode on")
	} else {
		log.SetLevel("info")
	}
	configFile, err := getConfigDir()
	if err != nil {
		log.Error(err)
		return
	}
	configFile = path.Join(configFile, "send.json")

	crocOptions := croc.Options{
		SharedSecret: c.String("code"),
		IsSender:     true,
		Debug:        c.GlobalBool("debug"),
		NoPrompt:     c.GlobalBool("yes"),
		RelayAddress: c.GlobalString("relay"),
		Stdout:       c.GlobalBool("stdout"),
		DisableLocal: c.Bool("no-local"),
		RelayPorts:   strings.Split(c.String("ports"), ","),
	}
	b, errOpen := ioutil.ReadFile(configFile)
	if errOpen == nil && !c.GlobalBool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly set
		if !c.GlobalIsSet("relay") {
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
	}

	var fnames []string
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		f, err := ioutil.TempFile(".", "croc-stdin-")
		if err != nil {
			return err
		}
		_, err = io.Copy(f, os.Stdin)
		if err != nil {
			return err
		}
		err = f.Close()
		if err != nil {
			return err
		}
		fnames = []string{f.Name()}
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

	haveFolder := false
	paths := []string{}
	for _, fname := range fnames {
		stat, err := os.Stat(fname)
		if err != nil {
			return err
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
				return err
			}
		} else {
			paths = append(paths, filepath.ToSlash(fname))
		}
	}

	cr, err := croc.New(crocOptions)
	if err != nil {
		return
	}

	// save the config
	if c.GlobalBool("remember") {
		log.Debug("saving config file")
		var bConfig []byte
		// if the code wasn't set, don't save it
		if c.String("code") == "" {
			crocOptions.SharedSecret = ""
		}
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

	err = cr.Send(croc.TransferOptions{
		PathToFiles:      paths,
		KeepPathInRemote: haveFolder,
	})

	return
}

func receive(c *cli.Context) (err error) {
	crocOptions := croc.Options{
		SharedSecret: c.String("code"),
		IsSender:     false,
		Debug:        c.GlobalBool("debug"),
		NoPrompt:     c.GlobalBool("yes"),
		RelayAddress: c.GlobalString("relay"),
		Stdout:       c.GlobalBool("stdout"),
	}
	if c.Args().First() != "" {
		crocOptions.SharedSecret = c.Args().First()
	}

	// load options here
	if c.GlobalBool("debug") {
		log.SetLevel("debug")
		log.Debug("debug mode on")
	} else {
		log.SetLevel("info")
	}
	configFile, err := getConfigDir()
	if err != nil {
		log.Error(err)
		return
	}
	configFile = path.Join(configFile, "receive.json")
	b, errOpen := ioutil.ReadFile(configFile)
	if errOpen == nil && !c.GlobalBool("remember") {
		var rememberedOptions croc.Options
		err = json.Unmarshal(b, &rememberedOptions)
		if err != nil {
			log.Error(err)
			return
		}
		// update anything that isn't explicitly set
		if !c.GlobalIsSet("relay") {
			crocOptions.RelayAddress = rememberedOptions.RelayAddress
		}
		if !c.GlobalIsSet("yes") {
			crocOptions.NoPrompt = rememberedOptions.NoPrompt
		}
		if crocOptions.SharedSecret == "" {
			crocOptions.SharedSecret = rememberedOptions.SharedSecret
		}
	}

	if crocOptions.SharedSecret == "" {
		crocOptions.SharedSecret = utils.GetInput("Enter receive code: ")
	}
	if c.GlobalString("out") != "" {
		os.Chdir(c.GlobalString("out"))
	}

	cr, err := croc.New(crocOptions)
	if err != nil {
		return
	}

	// save the config
	if c.GlobalBool("remember") {
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
	debugString := "info"
	if c.GlobalBool("debug") {
		debugString = "debug"
	}
	ports := strings.Split(c.String("ports"), ",")
	tcpPorts := strings.Join(ports[1:], ",")
	for i, port := range ports {
		if i == 0 {
			continue
		}
		go func(portStr string) {
			err = tcp.Run(debugString, portStr)
			if err != nil {
				panic(err)
			}
		}(port)
	}
	return tcp.Run(debugString, ports[0], tcpPorts)
}
