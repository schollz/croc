package main

import croc "github.com/schollz/croc/src"

func main() {
	err := croc.RunRelay("8002")
	if err != nil {
		panic(err)
	}
}
