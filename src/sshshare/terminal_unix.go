//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

func startTerminal(ctx context.Context, command []string, directory string, initial WindowSize) (*terminalHub, error) {
	cmd, err := terminalCommand(command, directory)
	if err != nil {
		return nil, err
	}
	size := normalizeWindowSize(initial)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)})
	if err != nil {
		return nil, fmtError("start shared terminal", err)
	}
	hub := newTerminalHub(
		ctx,
		ptmx,
		func(size WindowSize) error {
			return pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)})
		},
		func() error {
			if cmd.Process == nil {
				return nil
			}
			return cmd.Process.Signal(syscall.SIGHUP)
		},
	)
	go func() {
		err := cmd.Wait()
		if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) && exitErr.ExitCode() == 0 {
			err = nil
		}
		hub.finish(err)
	}()
	return hub, nil
}

func terminalCommand(command []string, directory string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if len(command) > 0 {
		cmd = exec.Command(command[0], command[1:]...)
	} else {
		shell, err := loginShell()
		if err != nil {
			return nil, err
		}
		cmd = exec.Command(shell, "-l")
	}
	if directory != "" {
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return nil, fmtError("resolve shared terminal directory", err)
		}
		cmd.Dir = absolute
	}
	cmd.Env = append(os.Environ(), "CROC_SSH_SESSION=1")
	if os.Getenv("TERM") == "" {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	return cmd, nil
}

func loginShell() (string, error) {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, nil
	}
	current, err := user.Current()
	if err != nil {
		return "", fmtError("find current user", err)
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("dscl", ".", "-read", filepath.Join("/Users", current.Username), "UserShell").Output()
		if err == nil {
			if shell, ok := strings.CutPrefix(string(out), "UserShell: "); ok {
				return strings.TrimSpace(shell), nil
			}
		}
	}
	return "/bin/sh", nil
}

func normalizePTYReadError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO) {
		return nil
	}
	return err
}

func fmtError(operation string, err error) error {
	return errors.New(operation + ": " + err.Error())
}
