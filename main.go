package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/schollz/croc/src/croc"
	"github.com/schollz/croc/src/utils"
	"github.com/urfave/cli"
)

var version string
var codePhrase string

var cr *croc.Croc

func main() {
	app := cli.NewApp()
	app.Name = "croc"
	if version == "" {
		version = "dev"
	}

	app.Version = version
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
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "relay", Value: "ws://croc4.schollz.com"},
		cli.BoolFlag{Name: "no-local", Usage: "disable local mode"},
		cli.BoolFlag{Name: "local", Usage: "use only local mode"},
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity (a lot)"},
		cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		cli.StringFlag{Name: "port", Value: "8153", Usage: "port that the websocket listens on"},
		cli.StringFlag{Name: "curve", Value: "siec", Usage: "specify elliptic curve to use (p224, p256, p384, p521, siec)"},
	}
	app.EnableBashCompletion = true
	app.HideHelp = false
	app.HideVersion = false
	app.BashComplete = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "send\nreceive\relay")
	}
	app.Action = func(c *cli.Context) error {
		return receive(c)
	}
	app.Before = func(c *cli.Context) error {
		cr = croc.Init(c.GlobalBool("debug"))
		cr.AllowLocalDiscovery = true
		cr.WebsocketAddress = c.GlobalString("relay")
		cr.Yes = c.GlobalBool("yes")
		cr.Stdout = c.GlobalBool("stdout")
		cr.LocalOnly = c.GlobalBool("local")
		cr.NoLocal = c.GlobalBool("no-local")
		cr.ShowText = true
		cr.ServerPort = c.String("port")
		cr.CurveType = c.String("curve")
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Printf("\nerror: %s", err.Error())
	}
}

func send(c *cli.Context) error {
	stat, _ := os.Stdin.Stat()
	var fname string
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		fname = "stdin"
	} else {
		fname = c.Args().First()
	}
	if fname == "" {
		return errors.New("must specify file: croc send [filename]")
	}
	cr.UseCompression = !c.Bool("no-compress")
	cr.UseEncryption = !c.Bool("no-encrypt")
	if c.String("code") != "" {
		codePhrase = c.String("code")
	}
	if len(codePhrase) == 0 {
		// generate code phrase
		codePhrase = utils.GetRandomName()
	}

	// print the text
	finfo, err := os.Stat(fname)
	if err != nil {
		return err
	}
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
		"Sending %s %s name '%s'\nCode is: %s\nOn the other computer, please run:\n\ncroc %s\n\n",
		humanize.Bytes(uint64(fsize)),
		fileOrFolder,
		filename,
		codePhrase,
		codePhrase,
	)
	err = cr.Send(fname, codePhrase)
	return err
}

func receive(c *cli.Context) error {
	if c.GlobalString("code") != "" {
		codePhrase = c.GlobalString("code")
	}
	if c.Args().First() != "" {
		codePhrase = c.Args().First()
	}
	if codePhrase == "" {
		codePhrase = utils.GetInput("Enter receive code: ")
	}
	return cr.Receive(codePhrase)
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
