package main

import croc "github.com/schollz/croc/src"

func main() {
	c := croc.Init()
	err := c.Relay()
	if err != nil {
		panic(err)
	}
}
