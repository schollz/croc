package main

import (
	"os"
	"time"

	"github.com/schollz/progressbar"
)

func main() {

	bar := progressbar.New(1000)

	// options for themes
	// theme, _ := themes.NewDefault(1)
	// theme := themes.New("~")
	// bar.SetTheme(theme)

	bar.Reset()
	for i := 0; i < 1000; i++ {
		bar.Add(1)
		time.Sleep(5 * time.Millisecond)
	}

	bar.Reset()
	bar.SetWriter(os.Stderr)
	for i := 0; i < 1000; i++ {
		bar.Add(1)
		time.Sleep(5 * time.Millisecond)
	}

}
