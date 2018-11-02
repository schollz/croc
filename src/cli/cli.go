package cli

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/schollz/croc/src/croc"
	"github.com/schollz/croc/src/utils"
	"github.com/skratchdot/open-golang/open"
	"github.com/urfave/cli"
)

var Version string
var cr *croc.Croc

func Run() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	app := cli.NewApp()
	app.Name = "croc"
	if Version == "" {
		Version = "dev"
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
				cli.BoolFlag{Name: "no-compress, o", Usage: "disable compression"},
				cli.BoolFlag{Name: "no-encrypt, e", Usage: "disable encryption"},
				cli.StringFlag{Name: "code, c", Usage: "codephrase used to connect to relay"},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return send(c)
			},
		},
		{
			Name:        "relay",
			Usage:       "start a croc relay",
			Description: "the croc relay will handle websocket and TCP connections",
			Flags:       []cli.Flag{},
			HelpName:    "croc relay",
			Action: func(c *cli.Context) error {
				return relay(c)
			},
		},
		{
			Name:        "config",
			Usage:       "generates a config file",
			Description: "the croc config can be used to set static parameters",
			Flags:       []cli.Flag{},
			HelpName:    "croc config",
			Action: func(c *cli.Context) error {
				return saveDefaultConfig(c)
			},
		},
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "addr", Value: "croc4.schollz.com", Usage: "address of the public relay"},
		cli.StringFlag{Name: "addr-ws", Value: "8153", Usage: "port of the public relay websocket server to connect"},
		cli.StringFlag{Name: "addr-tcp", Value: "8154,8155,8156,8157,8158,8159,8160,8161", Usage: "tcp ports of the public relay server to connect"},
		cli.BoolFlag{Name: "no-local", Usage: "disable local mode"},
		cli.BoolFlag{Name: "local", Usage: "use only local mode"},
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity (a lot)"},
		cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		cli.BoolFlag{Name: "force-tcp", Usage: "force TCP"},
		cli.BoolFlag{Name: "force-web", Usage: "force websockets"},
		cli.StringFlag{Name: "port", Value: "8153", Usage: "port that the websocket listens on"},
		cli.StringFlag{Name: "tcp-port", Value: "8154,8155,8156,8157,8158,8159,8160,8161", Usage: "ports that the tcp server listens on"},
		cli.StringFlag{Name: "curve", Value: "siec", Usage: "specify elliptic curve to use for PAKE (p256, p384, p521, siec)"},
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
	app.Before = func(c *cli.Context) error {
		cr = croc.Init(c.GlobalBool("debug"))
		cr.Version = Version
		cr.AllowLocalDiscovery = true
		cr.Address = c.GlobalString("addr")
		cr.AddressTCPPorts = strings.Split(c.GlobalString("addr-tcp"), ",")
		cr.AddressWebsocketPort = c.GlobalString("addr-ws")
		cr.NoRecipientPrompt = c.GlobalBool("yes")
		cr.Stdout = c.GlobalBool("stdout")
		cr.LocalOnly = c.GlobalBool("local")
		cr.NoLocal = c.GlobalBool("no-local")
		cr.ShowText = true
		cr.RelayWebsocketPort = c.String("port")
		cr.RelayTCPPorts = strings.Split(c.String("tcp-port"), ",")
		cr.CurveType = c.String("curve")
		if c.GlobalBool("force-tcp") {
			cr.ForceSend = 2
		}
		if c.GlobalBool("force-web") {
			cr.ForceSend = 1
		}
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\r\n%s", err.Error())
	}
	fmt.Fprintf(os.Stderr, "\r\n")
}

func saveDefaultConfig(c *cli.Context) error {
	return croc.SaveDefaultConfig()
}

func send(c *cli.Context) error {
	stat, _ := os.Stdin.Stat()
	var fname string
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
		fname = f.Name()
		defer func() {
			err = os.Remove(fname)
			if err != nil {
				log.Println(err)
			}
		}()
	} else {
		fname = c.Args().First()
	}
	if fname == "" {
		return errors.New("must specify file: croc send [filename]")
	}
	cr.UseCompression = !c.Bool("no-compress")
	cr.UseEncryption = !c.Bool("no-encrypt")
	if c.String("code") != "" {
		cr.Codephrase = c.String("code")
	}
	cr.LoadConfig()
	if len(cr.Codephrase) == 0 {
		// generate code phrase
		cr.Codephrase = utils.GetRandomName()
	}

	// print the text
	finfo, err := os.Stat(fname)
	if err != nil {
		return err
	}
	fname, _ = filepath.Abs(fname)
	fname = filepath.Clean(fname)
	_, filename := filepath.Split(fname)
	fileOrFolder := "file"
	fsize := finfo.Size()
	if finfo.IsDir() {
		fileOrFolder = "folder"
		fsize, err = dirSize(fname)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr,
		"Sending %s %s named '%s'\nCode is: %s\nOn the other computer, please run:\n\ncroc %s\n\n",
		humanize.Bytes(uint64(fsize)),
		fileOrFolder,
		filename,
		cr.Codephrase,
		cr.Codephrase,
	)
	if cr.Debug {
		croc.SetDebugLevel("debug")
	}
	return cr.Send(fname, cr.Codephrase)
}

func receive(c *cli.Context) error {
	if c.GlobalString("code") != "" {
		cr.Codephrase = c.GlobalString("code")
	}
	if c.Args().First() != "" {
		cr.Codephrase = c.Args().First()
	}
	if c.GlobalString("out") != "" {
		os.Chdir(c.GlobalString("out"))
	}
	cr.LoadConfig()
	openFolder := false
	if len(os.Args) == 1 {
		// open folder since they didn't give any arguments
		openFolder = true
	}
	if cr.Codephrase == "" {
		cr.Codephrase = utils.GetInput("Enter receive code: ")
	}
	if cr.Debug {
		croc.SetDebugLevel("debug")
	}
	err := cr.Receive(cr.Codephrase)
	if err == nil && openFolder {
		cwd, _ := os.Getwd()
		open.Run(cwd)
	}
	return err
}

func relay(c *cli.Context) error {
	return cr.Relay()
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}
