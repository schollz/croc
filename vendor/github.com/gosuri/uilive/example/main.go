package main

import (
	"fmt"
	"time"

	"github.com/gosuri/uilive"
)

func main() {
	writer := uilive.New()

	// start listening for updates and render
	writer.Start()

	for _, f := range []string{"Foo.zip", "Bar.iso"} {
		for i := 0; i <= 50; i++ {
			fmt.Fprintf(writer, "Downloading %s.. (%d/%d) GB\n", f, i, 50)
			time.Sleep(time.Millisecond * 25)
		}
		fmt.Fprintf(writer.Bypass(), "Downloaded %s\n", f)
	}

	fmt.Fprintln(writer, "Finished: Downloaded 100GB")
	writer.Stop() // flush and stop rendering
}
