package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli"
	"github.com/yudai/gotty/pkg/homedir"
)

const BUFFERSIZE = 1024

type AppConfig struct {
	HideLogo            bool   `yaml:"hidelogo"  flagName:"hidelogo" flagSName:"hl" flagDescribe:"Hidden logo" default:"false"`
	Relay               bool   `yaml:"relay"  flagName:"relay" flagSName:"r" flagDescribe:"Run as relay" default:"false"`
	Debug               bool   `yaml:"debug"  flagName:"debug" flagSName:"d" flagDescribe:"Debug mode" default:"false"`
	Wait                bool   `yaml:"wait"  flagName:"wait" flagSName:"w" flagDescribe:"Wait for code to be sent" default:"false"`
	PathSpec            bool   `yaml:"ask-save"  flagName:"ask-save" flagSName:"q" flagDescribe:"Ask for path to save to" default:"false"`
	DontEncrypt         bool   `yaml:"no-encrypt"  flagName:"no-encrypt" flagSName:"g" flagDescribe:"Turn off encryption" default:"false"`
	UseStdout           bool   `yaml:"stdout"  flagName:"stdout" flagSName:"o" flagDescribe:"Use stdout" default:"false"`
	Yes                 bool   `yaml:"yes"  flagName:"yes" flagSName:"y" flagDescribe:"Automatically accept file" default:"false"`
	Local               bool   `yaml:"local"  flagName:"local" flagSName:"lo" flagDescribe:"Use local relay when sending" default:"false"`
	Server              string `yaml:"server"  flagName:"server" flagSName:"l" flagDescribe:"Croc relay to use" default:"cowyo.com"`
	File                string `yaml:"send"  flagName:"send" flagSName:"s" flagDescribe:"File to send default:""`
	Path                string `yaml:"save"  flagName:"save" flagSName:"p" flagDescribe:"Path to save to" default:""`
	Code                string `yaml:"code"  flagName:"code" flagSName:"c" flagDescribe:"Use your own code phrase" default:""`
	Rate                int    `yaml:"rate"  flagName:"rate" flagSName:"R" flagDescribe:"Throttle down to speed in kbps" default:"1000000"`
	NumberOfConnections int    `yaml:"threads"  flagName:"threads" flagSName:"n" flagDescribe:"Number of threads to use" default:"4"`
}

var email string
var author string
var version string

func main() {

	app := cli.NewApp()
	app.Name = "croc"
	app.Version = version
	app.Author = author
	app.Email = email
	app.Usage = "send file by croc bridge"
	app.HideHelp = true

	cli.AppHelpTemplate = helpTemplate

	appOptions := &AppConfig{}
	if err := ApplyDefaultValues(appOptions); err != nil {
		exit(err, 1)
	}

	cliFlags, flagMappings, err := GenerateFlags(appOptions)
	if err != nil {
		exit(err, 3)
	}

	app.Flags = append(
		cliFlags,
		cli.StringFlag{
			Name:   "config",
			Value:  "~/.croc",
			Usage:  "Config file path",
			EnvVar: "CROC_CONFIG",
		},
	)

	app.Action = func(c *cli.Context) {

		configFile := c.String("config")
		_, err := os.Stat(homedir.Expand(configFile))
		if configFile != "~/.croc" || !os.IsNotExist(err) {
			if err := ApplyConfigFileYaml(configFile, appOptions); err != nil {
				exit(err, 2)
			}
		}

		ApplyFlags(cliFlags, flagMappings, c, appOptions)
		if appOptions.UseStdout {
			appOptions.HideLogo = true
		}
		if !appOptions.HideLogo {
			fmt.Println(`
                                ,_
                               >' )
   croc version ` + fmt.Sprintf("%5s", version) + `          ( ( \
                                || \
                 /^^^^\         ||
    /^^\________/0     \        ||
   (                    ` + "`" + `~+++,,_||__,,++~^^^^^^^
 ...V^V^V^V^V^V^\...............................

	`)
		}

		if appOptions.Relay {
			fmt.Println("running relay on local address " + GetLocalIP())
			r := NewRelay(appOptions)
			r.Run()
		} else {
			if appOptions.Local {
				fmt.Println("running relay on local address " + GetLocalIP())
				appOptions.Relay = true
				appOptions.Server = GetLocalIP()
				appOptions.DontEncrypt = true
				r := NewRelay(appOptions)
				go r.Run()
				appOptions.Code = "8-local"
				time.Sleep(500 * time.Millisecond)
			}
			c, err := NewConnection(appOptions)
			if err != nil {
				fmt.Printf("Error! Please submit the following error to https://github.com/schollz/croc/issues:\n\n'%s'\n\n", err.Error())
				return
			}
			err = c.Run()
			if err != nil {
				fmt.Printf("Error! Please submit the following error to https://github.com/schollz/croc/issues:\n\n'%s'\n\n", err.Error())
			}
		}
	}

	app.Run(os.Args)
}

func getInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "%s", prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func exit(err error, code int) {
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(code)
}
