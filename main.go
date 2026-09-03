package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/schollz/croc/v11/src/cli"
	"github.com/schollz/croc/v11/src/utils"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		errCh <- cli.RunContext(ctx)
	}()

	var err error
	select {
	case err = <-errCh:
	case <-ctx.Done():
		// Context-aware commands (including croc ssh) get a chance to restore
		// terminal state, revoke access, and stop their child process. Older
		// commands still retain the prompt exit behavior after this grace period.
		select {
		case err = <-errCh:
		case <-time.After(2 * time.Second):
		}
	}
	utils.RemoveMarkedFiles()
	if err != nil && err != context.Canceled {
		fmt.Println(err)
		os.Exit(1)
	}
}
