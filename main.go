package main

import (
	"flag"

	croc "github.com/schollz/croc/src"
)

func main() {
	var err error
	role := flag.Int("role", 0, "role number")
	passphrase := flag.String("code", "chou", "codephrase")
	flag.Parse()

	c := croc.Init()
	if *role == -1 {
		err = c.Relay()
	} else if *role == 0 {
		err = c.Send("croc.exe", *passphrase)
	} else {
		err = c.Receive(*passphrase)
	}
	if err != nil {
		panic(err)
	}
}
