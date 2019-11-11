package main

import (
	"flag"

	"github.com/schollz/croc/v7/src/relay"
	log "github.com/schollz/logger"
)

func main() {
	var startRelay bool
	flag.BoolVar(&startRelay, "relay", false, "start relay")
	flag.Parse()
	log.SetLevel("debug")
	if startRelay {
		relay.Run()
	}
}
