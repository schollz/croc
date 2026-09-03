//go:build !croc_no_tailcat && windows

package sshshare

import (
	"context"
	"os"

	internalssh "github.com/schollz/croc/v11/internal/sshclient"
)

func watchWindowChanges(context.Context, *os.File) (<-chan internalssh.WindowSize, func()) {
	return nil, func() {}
}
