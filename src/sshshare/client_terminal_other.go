//go:build !croc_no_tailcat && windows

package sshshare

import (
	"context"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

func watchWindowChanges(context.Context, *os.File, *gossh.Session) func() {
	return func() {}
}
