//go:build !croc_no_tailcat && windows

package sshshare

import (
	"context"
	"errors"
)

func startTerminal(context.Context, []string, string, WindowSize) (*terminalHub, error) {
	return nil, errors.New("hosting croc ssh is not supported on this platform")
}

func normalizePTYReadError(err error) error { return err }
