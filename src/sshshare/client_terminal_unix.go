//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func watchWindowChanges(ctx context.Context, terminal *os.File, session *gossh.Session) func() {
	resizeCtx, cancel := context.WithCancel(ctx)
	if terminal == nil || !term.IsTerminal(int(terminal.Fd())) {
		return cancel
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-signals:
				width, height, err := term.GetSize(int(terminal.Fd()))
				if err == nil {
					_ = session.WindowChange(height, width)
				}
			case <-resizeCtx.Done():
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		cancel()
	}
}
