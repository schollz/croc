package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

const BUFFERSIZE = 1024

var oneGigabytePerSecond = 1000000 // expressed as kbps

type Flags struct {
	HideLogo            bool
	Relay               bool
	Debug               bool
	Wait                bool
	PathSpec            bool
	DontEncrypt         bool
	Server              string
	File                string
	Path                string
	Code                string
	Rate                int
	NumberOfConnections int
}

var version string

func main() {
	flags := new(Flags)
	flag.BoolVar(&flags.HideLogo, "hidelogo", false, "run as relay")
	flag.BoolVar(&flags.Relay, "relay", false, "run as relay")
	flag.BoolVar(&flags.Debug, "debug", false, "debug mode")
	flag.BoolVar(&flags.Wait, "wait", false, "wait for code to be sent")
	flag.BoolVar(&flags.PathSpec, "ask-save", false, "ask for path to save to")
	flag.StringVar(&flags.Server, "server", "cowyo.com", "address of relay server")
	flag.StringVar(&flags.File, "send", "", "file to send")
	flag.StringVar(&flags.Path, "save", "", "path to save to")
	flag.StringVar(&flags.Code, "code", "", "use your own code phrase")
	flag.IntVar(&flags.Rate, "rate", oneGigabytePerSecond, "throttle down to speed in kbps")
	flag.BoolVar(&flags.DontEncrypt, "no-encrypt", false, "turn off encryption")
	flag.IntVar(&flags.NumberOfConnections, "threads", 4, "number of threads to use")
	flag.Parse()
	if !flags.HideLogo {
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
	fmt.Printf("croc version %s\n", version)

	if flags.Relay {
		r := NewRelay(flags)
		r.Run()
	} else {
		c, err := NewConnection(flags)
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

func getInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}
