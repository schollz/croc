package main

import (
	"flag"

	croc "github.com/schollz/croc/src"
)

func main() {
	var err error
	role := flag.Int("role", 0, "role number")
	flag.Parse()

	c := croc.Init()
	if *role == 0 {
		err = c.Relay()
	} else if *role == 1 {
		err = c.Send("foo")
	}
	if err != nil {
		panic(err)
	}
}
