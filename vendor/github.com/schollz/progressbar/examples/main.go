package main

import (
	"time"

	"github.com/schollz/progressbar"
)

func main() {
	bar := progressbar.New(1000)
	bar.Reset()
	for i := 0; i < 1000; i++ {
		bar.Add(1)
		time.Sleep(10 * time.Millisecond)
	}
}
