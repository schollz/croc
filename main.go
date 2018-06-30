package main

import (
	"flag"
	"fmt"

	croc "github.com/schollz/croc/src"
)

func main() {
	var err error
	role := flag.Int("role", 0, "role number")
	passphrase := flag.String("code", "chou", "codephrase")
	fname := flag.String("file", "", "codephrase")
	flag.Parse()

	c := croc.Init()
	// croc.SetLogLevel("error")
	if *role == -1 {
		err = c.Relay()
	} else if *role == 0 {
		err = c.Send(*fname, *passphrase)
	} else {
		err = c.Receive(*passphrase)
	}
	if err != nil {
		fmt.Print("Error: ")
		fmt.Println(err.Error())
	}
}
