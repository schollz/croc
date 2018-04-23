package main

import (
	"log"
	"time"

	"github.com/schollz/peerdiscovery"
)

func main() {
	p, err := peerdiscovery.New(peerdiscovery.Settings{
		Limit:     -1,
		Payload:   []byte(peerdiscovery.RandStringBytesMaskImprSrc(10)),
		Delay:     500 * time.Millisecond,
		TimeLimit: 10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	discoveries, err := p.Discover()
	if err != nil {
		log.Fatal(err)
	} else {
		for _, d := range discoveries {
			log.Printf("discovered ip '%s' with payload '%s'", d.Address, d.Payload)
		}
	}
}
