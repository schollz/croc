//go:build !croc_no_tailcat && windows

package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
)

var ErrDetached = errors.New("detached from shared SSH terminal")

func (h *Host) AttachLocalTerminal(context.Context, *os.File, io.Writer) error {
	return errors.New("hosting croc ssh is not supported on this platform")
}
