package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"

	log "github.com/cihub/seelog"
	"github.com/fatih/structs"
	"github.com/urfave/cli"
	"github.com/yudai/gotty/pkg/homedir"
	"github.com/yudai/hcl"
	yaml "gopkg.in/yaml.v2"
)

const BUFFERSIZE = 1024

type AppConfig struct {
	Relay               bool   `yaml:"relay"  flagName:"relay" flagSName:"r" flagDescribe:"Run as relay" default:"false"`
	Debug               bool   `yaml:"debug"  flagName:"debug" flagSName:"d" flagDescribe:"Debug mode" default:"false"`
	Wait                bool   `yaml:"wait"  flagName:"wait" flagSName:"w" flagDescribe:"Wait for code to be sent" default:"false"`
	PathSpec            bool   `yaml:"ask-save"  flagName:"ask-save" flagSName:"q" flagDescribe:"Ask for path to save to" default:"false"`
	DontEncrypt         bool   `yaml:"no-encrypt"  flagName:"no-encrypt" flagSName:"g" flagDescribe:"Turn off encryption" default:"false"`
	UseStdout           bool   `yaml:"stdout"  flagName:"stdout" flagSName:"o" flagDescribe:"Use stdout" default:"false"`
	Yes                 bool   `yaml:"yes"  flagName:"yes" flagSName:"y" flagDescribe:"Automatically accept file" default:"false"`
	Local               bool   `yaml:"local"  flagName:"local" flagSName:"lo" flagDescribe:"Use local relay when sending" default:"false"`
	NoLocal             bool   `yaml:"no-local"  flagName:"no-local" flagSName:"nlo" flagDescribe:"Don't create local relay" default:"false"`
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

func init() {

	SetLogLevel("debug")
}

func main() {
	defer log.Flush()
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

		if appOptions.Relay {
			fmt.Println("running relay on local address " + GetLocalIP())
			r := NewRelay(appOptions)
			r.Run()
		} else {
			c, err := NewConnection(appOptions)
			if err != nil {
				fmt.Printf("Error! Please submit the following error to https://github.com/schollz/croc/issues:\n\n'NewConnection: %s'\n\n", err.Error())
				return
			}
			err = c.Run()
			if err != nil {
				fmt.Printf("Error! Please submit the following error to https://github.com/schollz/croc/issues:\n\n'Run: %s'\n\n", err.Error())
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

func ApplyDefaultValues(struct_ interface{}) (err error) {
	o := structs.New(struct_)

	for _, field := range o.Fields() {
		defaultValue := field.Tag("default")
		if defaultValue == "" {
			continue
		}
		var val interface{}
		switch field.Kind() {
		case reflect.String:
			val = defaultValue
		case reflect.Bool:
			if defaultValue == "true" {
				val = true
			} else if defaultValue == "false" {
				val = false
			} else {
				return fmt.Errorf("invalid bool expression: %v, use true/false", defaultValue)
			}
		case reflect.Int:
			val, err = strconv.Atoi(defaultValue)
			if err != nil {
				return err
			}
		default:
			val = field.Value()
		}
		field.Set(val)
	}
	return nil
}

func GenerateFlags(options ...interface{}) (flags []cli.Flag, mappings map[string]string, err error) {
	mappings = make(map[string]string)

	for _, struct_ := range options {
		o := structs.New(struct_)
		for _, field := range o.Fields() {
			flagName := field.Tag("flagName")
			if flagName == "" {
				continue
			}
			envName := "CROC_" + strings.ToUpper(strings.Join(strings.Split(flagName, "-"), "_"))
			mappings[flagName] = field.Name()

			flagShortName := field.Tag("flagSName")
			if flagShortName != "" {
				flagName += ", " + flagShortName
			}

			flagDescription := field.Tag("flagDescribe")

			switch field.Kind() {
			case reflect.String:
				flags = append(flags, cli.StringFlag{
					Name:   flagName,
					Value:  field.Value().(string),
					Usage:  flagDescription,
					EnvVar: envName,
				})
			case reflect.Bool:
				flags = append(flags, cli.BoolFlag{
					Name:   flagName,
					Usage:  flagDescription,
					EnvVar: envName,
				})
			case reflect.Int:
				flags = append(flags, cli.IntFlag{
					Name:   flagName,
					Value:  field.Value().(int),
					Usage:  flagDescription,
					EnvVar: envName,
				})
			}
		}
	}

	return
}

func ApplyFlags(
	flags []cli.Flag,
	mappingHint map[string]string,
	c *cli.Context,
	options ...interface{},
) {
	objects := make([]*structs.Struct, len(options))
	for i, struct_ := range options {
		objects[i] = structs.New(struct_)
	}

	for flagName, fieldName := range mappingHint {
		if !c.IsSet(flagName) {
			continue
		}
		var field *structs.Field
		var ok bool
		for _, o := range objects {
			field, ok = o.FieldOk(fieldName)
			if ok {
				break
			}
		}
		if field == nil {
			continue
		}
		var val interface{}
		switch field.Kind() {
		case reflect.String:
			val = c.String(flagName)
		case reflect.Bool:
			val = c.Bool(flagName)
		case reflect.Int:
			val = c.Int(flagName)
		}
		field.Set(val)
	}
}

func ApplyConfigFile(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fileString := []byte{}
	log.Debugf("Loading config file at: %s", filePath)
	fileString, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	for _, object := range options {
		if err := hcl.Decode(object, string(fileString)); err != nil {
			return err
		}
	}

	return nil
}

func ApplyConfigFileYaml(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fileString := []byte{}
	log.Debugf("Loading config file at: %s", filePath)
	fileString, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	for _, object := range options {
		if err := yaml.Unmarshal(fileString, object); err != nil {
			return err
		}

	}

	return nil
}

func SaveConfigFileYaml(filePath string, options ...interface{}) error {
	filePath = homedir.Expand(filePath)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return err
	}

	fd, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer fd.Close()

	for _, object := range options {
		if byteString, err := yaml.Marshal(object); err != nil {
			return err
		} else {
			fd.Write(byteString)
		}
	}

	return nil
}

var helpTemplate = `
                                ,_
                               >' )
                               ( ( \
                                || \
                 /^^^^\         ||
    /^^\________/0     \        ||
   (                    ` + "`" + `~+++,,_||__,,++~^^^^^^^
 ...V^V^V^V^V^V^\...............................


NAME:
   {{.Name}} - {{.Usage}}

USAGE:
   {{.Name}} [options]

VERSION:
   {{.Version}}{{if or .Author .Email}}

AUTHOR:{{if .Author}}
  {{.Author}}{{if .Email}} - <{{.Email}}>{{end}}{{else}}
  {{.Email}}{{end}}{{end}}

OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}
`

// SetLogLevel determines the log level
func SetLogLevel(level string) (err error) {

	// https://en.wikipedia.org/wiki/ANSI_escape_code#3/4_bit
	// https://github.com/cihub/seelog/wiki/Log-levels
	appConfig := `
	<seelog minlevel="` + level + `">
	<outputs formatid="stdout">
	<filter levels="debug,trace">
		<console formatid="debug"/>
	</filter>
	<filter levels="info">
		<console formatid="info"/>
	</filter>
	<filter levels="critical,error">
		<console formatid="error"/>
	</filter>
	<filter levels="warn">
		<console formatid="warn"/>
	</filter>
	</outputs>
	<formats>
		<format id="stdout"   format="%Date %Time [%LEVEL] %File %FuncShort:%Line %Msg %n" />
		<format id="debug"   format="%Date %Time %EscM(37)[%LEVEL]%EscM(0) %File %FuncShort:%Line %Msg %n" />
		<format id="info"    format="%EscM(36)[%LEVEL]%EscM(0) %Msg %n" />
		<format id="warn"    format="%Date %Time %EscM(33)[%LEVEL]%EscM(0) %File %FuncShort:%Line %Msg %n" />
		<format id="error"   format="%Date %Time %EscM(31)[%LEVEL]%EscM(0) %File %FuncShort:%Line %Msg %n" />
	</formats>
	</seelog>
	`
	logger, err := log.LoggerFromConfigAsBytes([]byte(appConfig))
	if err != nil {
		return
	}
	log.ReplaceLogger(logger)
	return
}
