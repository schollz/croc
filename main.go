package main

import (
	"flag"

	"github.com/schollz/croc/v7/src/croc"
	"github.com/schollz/croc/v7/src/relay"
	log "github.com/schollz/logger"
)

func main() {
	var startRelay, startSend, startReceive bool
	flag.BoolVar(&startRelay, "relay", false, "start relay")
	flag.BoolVar(&startSend, "send", false, "send")
	flag.BoolVar(&startReceive, "receive", false, "receive")
	flag.Parse()
	log.SetLevel("debug")
	if startRelay {
		relay.Run()
	} else if startSend {
		c, err := croc.New(croc.Options{
			IsSender:     true,
			SharedSecret: "test1",
			RelayAddress: "wss://testcroc.schollz.com/ws",
			Debug:        true,
		})
		if err != nil {
			panic(err)
		}
		err = c.Send(croc.TransferOptions{})
		if err != nil {
			panic(err)
		}
	} else if startReceive {
		c, err := croc.New(croc.Options{
			IsSender:     false,
			SharedSecret: "test1",
			RelayAddress: "wss://testcroc.schollz.com/ws",
			Debug:        true,
		})
		if err != nil {
			panic(err)
		}
		err = c.Receive()
		if err != nil {
			panic(err)
		}
	}
}
