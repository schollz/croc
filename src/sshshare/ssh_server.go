//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"fmt"

	ssh "github.com/tailscale/gliderssh"
	gossh "golang.org/x/crypto/ssh"
)

func newSharedSSHServer(hub *terminalHub, signer gossh.Signer, role Role, beginAttachment func(Role) func()) *ssh.Server {
	writable := role == RoleReadWrite
	server := &ssh.Server{
		NoClientAuthHandler: func(ssh.Context) error { return nil },
		ChannelHandlers:     map[string]ssh.ChannelHandler{"session": ssh.DefaultSessionHandler},
		RequestHandlers:     map[string]ssh.RequestHandler{},
		SubsystemHandlers:   map[string]ssh.SubsystemHandler{},
		HandshakeTimeout:    sshHandshakeTimeout,
	}
	server.Handler = func(session ssh.Session) {
		if session.RawCommand() != "" {
			_, _ = fmt.Fprintln(session.Stderr(), "croc ssh shares one interactive terminal; remote commands are disabled")
			_ = session.Exit(1)
			return
		}
		ptyRequest, windows, ok := session.Pty()
		if !ok {
			_, _ = fmt.Fprintln(session.Stderr(), "croc ssh requires an interactive terminal")
			_ = session.Exit(1)
			return
		}
		session.DisablePTYEmulation()
		resize := make(chan WindowSize, 8)
		go func() {
			defer close(resize)
			for window := range windows {
				select {
				case resize <- WindowSize{Width: window.Width, Height: window.Height}:
				case <-session.Context().Done():
					return
				}
			}
		}()
		if beginAttachment != nil {
			done := beginAttachment(role)
			if done == nil {
				_ = session.Exit(1)
				return
			}
			defer done()
		}
		err := hub.Attach(
			session.Context(), session, session, writable,
			WindowSize{Width: ptyRequest.Window.Width, Height: ptyRequest.Window.Height},
			resize,
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintf(session.Stderr(), "\r\ncroc ssh session ended: %v\r\n", err)
			_ = session.Exit(1)
			return
		}
		_ = session.Exit(0)
	}
	server.AddHostKey(signer)
	return server
}
