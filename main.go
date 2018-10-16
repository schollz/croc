package main

import "github.com/schollz/croc/src/cli"

var Version string

func main() {
	cli.Version = Version
	cli.Run()
}
