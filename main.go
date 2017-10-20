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
	Relay               bool
	Debug               bool
	Wait                bool
	DontEncrypt         bool
	Server              string
	File                string
	Code                string
	Rate                int
	NumberOfConnections int
}

var version string

func main() {
	fmt.Println(`
         /\_/\
    ____/ o o \
  /~____  =ø= /
 (______)__m_m)

croc version ` + version + `
`)
	flags := new(Flags)
	flag.BoolVar(&flags.Relay, "relay", false, "run as relay")
	flag.BoolVar(&flags.Debug, "debug", false, "debug mode")
	flag.BoolVar(&flags.Wait, "wait", false, "wait for code to be sent")
	flag.StringVar(&flags.Server, "server", "cowyo.com", "address of relay server")
	flag.StringVar(&flags.File, "send", "", "file to send")
	flag.StringVar(&flags.Code, "code", "", "use your own code phrase")
	flag.IntVar(&flags.Rate, "rate", oneGigabytePerSecond, "throttle down to speed in kbps")
	flag.BoolVar(&flags.DontEncrypt, "no-encrypt", false, "turn off encryption")
	flag.IntVar(&flags.NumberOfConnections, "threads", 4, "number of threads to use")
	flag.Parse()

	if flags.Relay {
		r := NewRelay(flags)
		r.Run()
	} else {
		c := NewConnection(flags)
		err := c.Run()
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
