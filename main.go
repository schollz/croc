package main

//go:generate go run src/install/updateversion.go
//go:generate git commit -am "bump $VERSION"
//go:generate git tag -af v$VERSION -m "v$VERSION"

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/schollz/croc/v10/src/cli"
	"github.com/schollz/croc/v10/src/utils"
)

func main() {
	// "github.com/pkg/profile"
	// go func() {
	// 	for {
	// 		f, err := os.Create("croc.pprof")
	// 		if err != nil {
	// 			panic(err)
	// 		}
	// 		runtime.GC() // get up-to-date statistics
	// 		if err := pprof.WriteHeapProfile(f); err != nil {
	// 			panic(err)
	// 		}
	// 		f.Close()
	// 		time.Sleep(3 * time.Second)
	// 		fmt.Println("wrote profile")
	// 	}
	// }()

	// Create a channel to receive OS signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := cli.Run(); err != nil {
			log.Fatalln(err)
		}
	}()

	// Wait for a termination signal
	sig := <-sigs
	log.Println("Received signal:", sig)

	// Perform any necessary cleanup here
	log.Println("Performing cleanup...")
	utils.CleanupTempData()

	// Exit the program gracefully
	os.Exit(0)
}
