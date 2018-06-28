package main

import croc "github.com/schollz/croc/src"

func main() {
	err := croc.Relay([]string{"27001", "27002", "27003", "27004"}, "8002")
	if err != nil {
		panic(err)
	}
}
