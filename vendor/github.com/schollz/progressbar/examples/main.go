package main

import (
	"time"

	ansi "github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar"
)

func main() {
	// basic bar
	bar := progressbar.New(1000)
	bar.RenderBlank() // will show the progress bar
	time.Sleep(1 * time.Second)
	for i := 0; i < 1000; i++ {
		bar.Add(1)
		time.Sleep(2 * time.Millisecond)
	}

	// // bar with options
	// bar = progressbar.NewOptions(1000,
	// 	progressbar.OptionSetWriter(os.Stderr),
	// 	progressbar.OptionSetTheme(progressbar.Theme{Saucer: "~", SaucerPadding: "-", BarStart: "|", BarEnd: "|"}),
	// )
	// for i := 0; i < 1000; i++ {
	// 	bar.Add(1)
	// 	time.Sleep(2 * time.Millisecond)
	// }

	bar = progressbar.NewOptions(100,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan][1/3][reset] Writing moshable file..."),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))
	for i := 0; i < 1000; i++ {
		bar.Add(1)
		time.Sleep(2 * time.Millisecond)
	}

}
