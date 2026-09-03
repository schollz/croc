//go:build !croc_no_tailcat && (linux || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

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
	process := newTerminalProcess(cmd)
	hub := newTerminalHub(
		ctx,
		ptmx,
		func(size WindowSize) error {
			return pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)})
		},
		process.Stop,
	)
	go func() {
		<-process.Done()
		hub.finish(process.Err())
	}()
	return hub, nil
}

type terminalProcess struct {
	cmd  *exec.Cmd
	done chan struct{}

	mu       sync.Mutex
	err      error
	stopping bool
	waited   bool
}

func newTerminalProcess(cmd *exec.Cmd) *terminalProcess {
	process := &terminalProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		process.mu.Lock()
		if process.stopping {
			err = nil
		}
		process.err = err
		process.waited = true
		process.mu.Unlock()
		close(process.done)
	}()
	return process
}

func (p *terminalProcess) Done() <-chan struct{} { return p.done }

func (p *terminalProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Stop terminates the whole PTY process group, first allowing a short grace
// period after SIGHUP and then forcing termination. cmd.Wait is owned by the
// terminalProcess goroutine, so Stop also guarantees the child is reaped.
func (p *terminalProcess) Stop() error {
	p.mu.Lock()
	alreadyWaited := p.waited
	waitErr := p.err
	if !alreadyWaited {
		p.stopping = true
	}
	p.mu.Unlock()
	if alreadyWaited {
		return waitErr
	}

	var stopErr error
	if err := signalProcessGroup(p.cmd, syscall.SIGHUP); err != nil {
		stopErr = fmt.Errorf("signal shared terminal: %w", err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-p.done:
		return stopErr
	case <-timer.C:
	}
	if err := signalProcessGroup(p.cmd, syscall.SIGKILL); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("kill shared terminal: %w", err))
	}
	<-p.done
	return stopErr
}

func signalProcessGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
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
	return fmt.Errorf("%s: %w", operation, err)
}
