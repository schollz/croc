//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	ssh "github.com/tailscale/gliderssh"
	gossh "golang.org/x/crypto/ssh"
)

type sharedSSHServer struct {
	server *ssh.Server
	now    func() time.Time

	authMu sync.Mutex
	grants map[[sha256.Size]byte]time.Time
	closed bool
}

func newSharedSSHServer(hub *terminalHub, signer gossh.Signer, role Role, beginAttachment func(Role) func()) *sharedSSHServer {
	writable := role == RoleReadWrite
	shared := &sharedSSHServer{
		now:    time.Now,
		grants: make(map[[sha256.Size]byte]time.Time),
	}
	server := &ssh.Server{
		PasswordHandler:   shared.authenticate,
		ChannelHandlers:   map[string]ssh.ChannelHandler{"session": ssh.DefaultSessionHandler},
		RequestHandlers:   map[string]ssh.RequestHandler{},
		SubsystemHandlers: map[string]ssh.SubsystemHandler{},
		HandshakeTimeout:  sshHandshakeTimeout,
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
	shared.server = server
	return shared
}

func (s *sharedSSHServer) HandleConn(connection net.Conn) {
	s.server.HandleConn(connection)
}

func (s *sharedSSHServer) AddClientAuth(clientAuth []byte, expiresAt time.Time) error {
	if err := validateSSHClientAuth(clientAuth); err != nil {
		return err
	}
	if expiresAt.IsZero() {
		return errors.New("SSH client authentication expiry is required")
	}
	now := s.now()
	if !now.Before(expiresAt) {
		return errors.New("SSH client authentication has expired")
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if s.closed {
		return errors.New("SSH server is closed")
	}
	s.pruneExpiredLocked(now)
	s.grants[sha256.Sum256(clientAuth)] = expiresAt
	return nil
}

func (s *sharedSSHServer) RevokeClientAuth(clientAuth []byte) {
	if len(clientAuth) != sshClientAuthSize {
		return
	}
	s.authMu.Lock()
	delete(s.grants, sha256.Sum256(clientAuth))
	s.authMu.Unlock()
}

func (s *sharedSSHServer) authenticate(_ ssh.Context, password string) bool {
	clientAuth, err := base64.RawStdEncoding.DecodeString(password)
	if err != nil || len(clientAuth) != sshClientAuthSize {
		return false
	}
	digest := sha256.Sum256(clientAuth)
	clear(clientAuth)
	now := s.now()
	s.authMu.Lock()
	expiresAt, ok := s.grants[digest]
	if ok {
		delete(s.grants, digest)
	}
	s.pruneExpiredLocked(now)
	s.authMu.Unlock()
	return ok && now.Before(expiresAt)
}

func (s *sharedSSHServer) pruneExpiredLocked(now time.Time) {
	for grant, expiry := range s.grants {
		if !now.Before(expiry) {
			delete(s.grants, grant)
		}
	}
}

func (s *sharedSSHServer) Close() error {
	s.authMu.Lock()
	s.closed = true
	clear(s.grants)
	s.authMu.Unlock()
	return s.server.Close()
}
