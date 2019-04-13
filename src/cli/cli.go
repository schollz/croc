package cli

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/schollz/croc/v5/src/croc"
	"github.com/schollz/croc/v5/src/utils"
	"github.com/urfave/cli"
)

var Version string
var cr *croc.Client

func Run() (err error) {

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
				cli.StringFlag{Name: "code, c", Usage: "codephrase used to connect to relay"},
			},
			HelpName: "croc send",
			Action: func(c *cli.Context) error {
				return send(c)
			},
		},
	}
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug", Usage: "increase verbosity (a lot)"},
		cli.BoolFlag{Name: "yes", Usage: "automatically agree to all prompts"},
		cli.BoolFlag{Name: "stdout", Usage: "redirect file to stdout"},
		cli.StringFlag{Name: "relay", Value: "198.199.67.130:6372", Usage: "address of the relay"},
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
		var err error
		cr, err = croc.New(croc.Options{
			Debug:        c.GlobalBool("debug"),
			NoPrompt:     c.GlobalBool("yes"),
			AddressRelay: c.GlobalString("relay"),
			Stdout:       c.GlobalBool("stdout"),
		})
		return err
	}

	return app.Run(os.Args)
}

// func saveDefaultConfig(c *cli.Context) error {
// 	return croc.SaveDefaultConfig()
// }

func send(c *cli.Context) (err error) {
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
				log.Println(err)
			}
		}()
	} else {
		fnames = append([]string{c.Args().First()}, c.Args().Tail()...)
	}
	if len(fnames) == 0 {
		return errors.New("must specify file: croc send [filename]")
	}

	if c.String("code") != "" {
		cr.Options.SharedSecret = c.String("code")
	}
	// cr.LoadConfig()
	if len(cr.Options.SharedSecret) == 0 {
		// generate code phrase
		cr.Options.SharedSecret = utils.GetRandomName()
	}

	paths := []string{}
	for _, fname := range fnames {
		stat, err := os.Stat(fname)
		if err != nil {
			return err
		}
		if stat.IsDir() {
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

	err = cr.Send(croc.TransferOptions{
		PathToFiles:      paths,
		KeepPathInRemote: false,
	})

	return
}

func receive(c *cli.Context) (err error) {
	if c.GlobalString("code") != "" {
		cr.Options.SharedSecret = c.GlobalString("code")
	}
	if c.Args().First() != "" {
		cr.Options.SharedSecret = c.Args().First()
	}
	if cr.Options.SharedSecret == "" {
		cr.Options.SharedSecret = utils.GetInput("Enter receive code: ")
	}
	if c.GlobalString("out") != "" {
		os.Chdir(c.GlobalString("out"))
	}

	err = cr.Receive()
	return
}

// func relay(c *cli.Context) error {
// 	return cr.Relay()
// }

// func dirSize(path string) (int64, error) {
// 	var size int64
// 	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
// 		if !info.IsDir() {
// 			size += info.Size()
// 		}
// 		return err
// 	})
// 	return size, err
// }
