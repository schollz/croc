//go:build !croc_no_tailcat && windows

package sshshare

import (
	"context"
	"io"
	"os"
)

func (h *Host) AttachLocalTerminal(context.Context, *os.File, io.Writer) error {
	return errHostingUnsupported
}
