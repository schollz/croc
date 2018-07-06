package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	croc "github.com/schollz/croc/src"
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
				cli.BoolFlag{Name: "no-compress, o"},
				cli.BoolFlag{Name: "no-encrypt, e"},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return send(c)
			},
		},
		{
			Name:        "receive",
			Usage:       "receive a file",
			Description: "receve a file over the relay",
			HelpName:    "croc receive",
			Action: func(c *cli.Context) error {
				return receive(c)
			},
		},
		{
			Name:        "relay",
			Usage:       "start a croc relay",
			Description: "the croc relay will handle websocket and TCP connections",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "tcp", Value: "27130,27131,27132,27133", Usage: "ports for the tcp connections"},
				cli.StringFlag{Name: "port", Value: "8130", Usage: "port that the websocket listens on"},
				cli.StringFlag{Name: "curve", Value: "siec", Usage: "specify elliptic curve to use (p224, p256, p384, p521, siec)"},
			},
			HelpName: "croc relay",
			Action: func(c *cli.Context) error {
				return relay(c)
			},
		},
	}
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "relay", Value: "wss://croc3.schollz.com"},
		cli.StringFlag{Name: "code, c", Usage: "codephrase used to connect to relay"},
		cli.BoolFlag{Name: "no-local", Usage: "disable local mode"},
		cli.BoolFlag{Name: "local", Usage: "use only local mode"},
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity (a lot)"},
		cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
	}
	app.EnableBashCompletion = true
	app.HideHelp = false
	app.HideVersion = false
	app.BashComplete = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "lipstick\nkiss\nme\nlipstick\nringo\n")
	}
	app.Action = func(c *cli.Context) error {
		return receive(c)
	}
	app.Before = func(c *cli.Context) error {
		cr = croc.Init()
		cr.AllowLocalDiscovery = true
		cr.WebsocketAddress = c.GlobalString("relay")
		cr.SetDebug(c.GlobalBool("debug"))
		cr.Yes = c.GlobalBool("yes")
		cr.Stdout = c.GlobalBool("stdout")
		cr.LocalOnly = c.GlobalBool("local")
		cr.NoLocal = c.GlobalBool("no-local")
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
	if c.GlobalString("code") != "" {
		codePhrase = c.GlobalString("code")
	}
	return cr.Send(fname, codePhrase)
}

func receive(c *cli.Context) error {
	if c.GlobalString("code") != "" {
		codePhrase = c.GlobalString("code")
	}
	if c.Args().First() != "" {
		codePhrase = c.Args().First()
	}
	return cr.Receive(codePhrase)
}

func relay(c *cli.Context) error {
	cr.TcpPorts = strings.Split(c.String("tcp"), ",")
	cr.ServerPort = c.String("port")
	cr.CurveType = c.String("curve")
	return cr.Relay()
}
