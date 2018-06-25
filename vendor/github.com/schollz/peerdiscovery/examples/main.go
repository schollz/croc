package main

import (
	"fmt"
	"log"
	math_rand "math/rand"
	"time"

	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar"
)

func main() {
	fmt.Println("Scanning for 10 seconds to find LAN peers")
	// show progress bar
	go func() {
		bar := progressbar.New(10)
		for i := 0; i < 10; i++ {
			bar.Add(1)
			time.Sleep(1 * time.Second)
		}
		fmt.Print("\n")
	}()

	// discover peers
	discoveries, err := peerdiscovery.Discover(peerdiscovery.Settings{
		Limit:     -1,
		Payload:   []byte(randStringBytesMaskImprSrc(10)),
		Delay:     500 * time.Millisecond,
		TimeLimit: 10 * time.Second,
	})

	// print out results
	if err != nil {
		log.Fatal(err)
	} else {
		if len(discoveries) > 0 {
			fmt.Printf("Found %d other computers\n", len(discoveries))
			for i, d := range discoveries {
				fmt.Printf("%d) '%s' with payload '%s'\n", i, d.Address, d.Payload)
			}
		} else {
			fmt.Println("Found no devices. You need to run this on another computer at the same time.")
		}
	}
}

// src is seeds the random generator for generating random strings
var src = math_rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// RandStringBytesMaskImprSrc prints a random string
func randStringBytesMaskImprSrc(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}
