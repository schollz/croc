//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	internalssh "github.com/schollz/croc/v11/internal/sshclient"
	"golang.org/x/term"
)

func watchWindowChanges(ctx context.Context, terminal *os.File) (<-chan internalssh.WindowSize, func()) {
	resizeCtx, cancel := context.WithCancel(ctx)
	if terminal == nil || !term.IsTerminal(int(terminal.Fd())) {
		return nil, cancel
	}
	signals := make(chan os.Signal, 1)
	resizes := make(chan internalssh.WindowSize, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		defer close(resizes)
		for {
			select {
			case <-signals:
				width, height, err := term.GetSize(int(terminal.Fd()))
				if err == nil {
					size := internalssh.WindowSize{Width: width, Height: height}
					select {
					case resizes <- size:
					default:
						select {
						case <-resizes:
						default:
						}
						select {
						case resizes <- size:
						default:
						}
					}
				}
			case <-resizeCtx.Done():
				return
			}
		}
	}()
	return resizes, func() {
		signal.Stop(signals)
		cancel()
	}
}
