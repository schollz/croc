package main

import (
	"flag"

	"github.com/schollz/croc/v5/src/croc"
)

func main() {
	var sender bool
	flag.BoolVar(&sender, "sender", false, "sender")
	flag.Parse()
	c, err := croc.New(sender, "foo")
	if err != nil {
		panic(err)
	}
	if sender {
		err = c.Send("test.txt")
	} else {
		err = c.Receive()
	}
	if err != nil {
		panic(err)
	}
}
