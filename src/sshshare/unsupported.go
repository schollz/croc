//go:build croc_no_tailcat || (!linux && !windows && !darwin && !freebsd && !openbsd)

// Package sshshare provides croc's collaborative terminal feature. This file
// preserves the CLI and library surface in builds that intentionally omit the
// Tailcat transport or target a platform Tailcat does not support.
package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
)

var errUnsupported = errors.New("croc ssh is not supported in this build")

type Host struct{}

func StartHost(context.Context, HostConfig) (*Host, error) { return nil, errUnsupported }
func (*Host) Code(Role) string                             { return "" }
func (*Host) Relay(Role) string                            { return "" }
func (*Host) AttachLocal(context.Context, io.Reader, io.Writer, WindowSize, <-chan WindowSize) error {
	return errUnsupported
}
func (*Host) AttachLocalTerminal(context.Context, *os.File, io.Writer) error { return errUnsupported }
func (*Host) Done() <-chan struct{}                                          { return nil }
func (*Host) Wait() error                                                    { return errUnsupported }
func (*Host) Close() error                                                   { return nil }

func Join(context.Context, ClientConfig) error { return errUnsupported }
