// Package sshclient contains the transport-independent half of croc's SSH
// client. Native and browser callers provide an already-authenticated byte
// stream plus their own terminal input and output adapters.
package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

const DefaultHandshakeTimeout = 30 * time.Second

type WindowSize struct {
	Width  int
	Height int
}

type Config struct {
	ExpectedHostKey  gossh.PublicKey
	TerminalName     string
	InitialSize      WindowSize
	Input            io.Reader
	PrepareInput     func() (io.Reader, func(), error)
	Output           io.Writer
	ErrorOutput      io.Writer
	Resizes          <-chan WindowSize
	HandshakeTimeout time.Duration
	BeforeShell      func() (func(), error)
	OnConnected      func()
}

// Run pins the authenticated host key, opens one interactive PTY, and waits
// for the shared shell or context to end. It intentionally exposes no remote
// commands, forwarding, or subsystems.
func Run(ctx context.Context, connection net.Conn, config Config) (bool, error) {
	if ctx == nil {
		return false, errors.New("SSH session context is required")
	}
	if connection == nil {
		return false, errors.New("SSH connection is required")
	}
	if config.ExpectedHostKey == nil {
		return false, errors.New("SSH host key is required")
	}
	if (config.Input == nil && config.PrepareInput == nil) || config.Output == nil || config.ErrorOutput == nil {
		return false, errors.New("SSH terminal streams are required")
	}
	if config.TerminalName == "" {
		config.TerminalName = "xterm-256color"
	}
	config.InitialSize = normalizeWindowSize(config.InitialSize)
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = DefaultHandshakeTimeout
	}

	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	handshakeDeadline := time.Now().Add(config.HandshakeTimeout)
	var contextHandshakeDeadline time.Time
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
		contextHandshakeDeadline = deadline
	}
	if err := connection.SetDeadline(handshakeDeadline); err != nil {
		return false, fmt.Errorf("set SSH handshake deadline: %w", err)
	}

	sshConfig := &gossh.ClientConfig{
		User:              "croc",
		HostKeyAlgorithms: []string{config.ExpectedHostKey.Type()},
		HostKeyCallback: func(_ string, _ net.Addr, actual gossh.PublicKey) error {
			if !bytes.Equal(actual.Marshal(), config.ExpectedHostKey.Marshal()) {
				return errors.New("SSH host key does not match authenticated invitation")
			}
			return nil
		},
	}
	clientConnection, channels, requests, err := gossh.NewClientConn(connection, "croc-ssh", sshConfig)
	if err != nil {
		return false, HandshakeError(ctx, contextHandshakeDeadline, err)
	}
	client := gossh.NewClient(clientConnection, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return false, fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	if err = session.RequestPty(config.TerminalName, config.InitialSize.Height, config.InitialSize.Width, gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return false, fmt.Errorf("request SSH terminal: %w", err)
	}
	input := config.Input
	cleanupInput := func() {}
	if config.PrepareInput != nil {
		input, cleanupInput, err = config.PrepareInput()
		if err != nil {
			return false, err
		}
		if cleanupInput == nil {
			cleanupInput = func() {}
		}
	}
	defer cleanupInput()
	if input == nil {
		return false, errors.New("SSH terminal input is required")
	}
	session.Stdin = input
	session.Stdout = config.Output
	session.Stderr = config.ErrorOutput

	cleanup := func() {}
	if config.BeforeShell != nil {
		cleanup, err = config.BeforeShell()
		if err != nil {
			return false, err
		}
		if cleanup == nil {
			cleanup = func() {}
		}
	}
	defer cleanup()

	if err = session.Shell(); err != nil {
		return false, fmt.Errorf("start shared SSH shell: %w", err)
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		return false, fmt.Errorf("clear SSH handshake deadline: %w", err)
	}
	if config.OnConnected != nil {
		config.OnConnected()
	}

	resizeCtx, stopResize := context.WithCancel(ctx)
	defer stopResize()
	if config.Resizes != nil {
		go func() {
			for {
				select {
				case size, ok := <-config.Resizes:
					if !ok {
						return
					}
					size = normalizeWindowSize(size)
					_ = session.WindowChange(size.Height, size.Width)
				case <-resizeCtx.Done():
					return
				}
			}
		}()
	}

	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()
	select {
	case err = <-wait:
		return true, err
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

func normalizeWindowSize(size WindowSize) WindowSize {
	if size.Width <= 0 {
		size.Width = 80
	}
	if size.Height <= 0 {
		size.Height = 24
	}
	return size
}

func HandshakeError(ctx context.Context, contextDeadline time.Time, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if !contextDeadline.IsZero() && !time.Now().Before(contextDeadline) {
		return context.DeadlineExceeded
	}
	return fmt.Errorf("SSH handshake: %w", err)
}
