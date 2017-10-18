package uilive_test

import (
	"fmt"
	"time"

	"github.com/gosuri/uilive"
)

func Example() {
	writer := uilive.New()

	// start listening to updates and render
	writer.Start()

	for i := 0; i <= 100; i++ {
		fmt.Fprintf(writer, "Downloading.. (%d/%d) GB\n", i, 100)
		time.Sleep(time.Millisecond * 5)
	}

	fmt.Fprintln(writer, "Finished: Downloaded 100GB")
	writer.Stop() // flush and stop rendering
}
