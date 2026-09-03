//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// AttachLocalTerminal attaches a real local terminal to the host session,
// maintaining raw mode and propagating window-size changes.
func (h *Host) AttachLocalTerminal(ctx context.Context, input *os.File, output io.Writer) error {
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return errors.New("hosting croc ssh interactively requires a terminal (use --headless otherwise)")
	}
	width, height, err := term.GetSize(int(input.Fd()))
	if err != nil {
		width, height = 80, 24
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return fmtError("make host terminal raw", err)
	}
	defer term.Restore(int(input.Fd()), state)

	resizes := make(chan WindowSize, 1)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	defer signal.Stop(signals)
	resizeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(resizes)
		for {
			select {
			case <-signals:
				newWidth, newHeight, sizeErr := term.GetSize(int(input.Fd()))
				if sizeErr == nil {
					select {
					case resizes <- WindowSize{Width: newWidth, Height: newHeight}:
					default:
					}
				}
			case <-resizeCtx.Done():
				return
			}
		}
	}()
	escape := &detachReader{reader: input}
	err = h.AttachLocal(
		ctx, escape, output,
		WindowSize{Width: width, Height: height}, resizes,
	)
	if escape.Stopped() {
		// Raw terminal mode turns Ctrl-C into an ordinary byte instead of a
		// SIGINT. Close explicitly so callers outside the CLI get the same
		// stop-session behavior: end the shell and revoke both invitations.
		return h.Close()
	}
	if escape.Detached() {
		return ErrDetached
	}
	return err
}
