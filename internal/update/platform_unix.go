//go:build !windows

package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func lockDirectory(directory string) (func(), error) {
	dir, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		dir.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errors.New("another croc installation is in progress; retry when it finishes")
		}
		return nil, err
	}
	return func() { unix.Flock(int(dir.Fd()), unix.LOCK_UN); dir.Close() }, nil
}

func setMetadata(file *os.File, info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		mine, err := file.Stat()
		if err != nil {
			return err
		}
		own := mine.Sys().(*syscall.Stat_t)
		if own.Uid != stat.Uid || own.Gid != stat.Gid {
			if err := file.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return err
			}
		}
	}
	mode := info.Mode().Perm()
	if info.IsDir() {
		mode = 0o755
	}
	return file.Chmod(mode)
}

func replaceFile(staged, target string, _ bool) error { return os.Rename(staged, target) }

type elevationRunner func(context.Context, string, []string) error

func elevate(ctx context.Context, executable string, args []string) error {
	return elevateUnix(ctx, executable, args, exec.LookPath, func(ctx context.Context, tool string, args []string) error {
		command := exec.CommandContext(ctx, tool, args...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		return command.Run()
	}, term.IsTerminal(int(os.Stdin.Fd())))
}

func elevateUnix(ctx context.Context, executable string, args []string, lookPath func(string) (string, error), run elevationRunner, interactive bool) error {
	if os.Geteuid() == 0 {
		return errors.New("installation is already elevated; check filesystem permissions and immutable flags")
	}
	for _, name := range []string{"sudo", "doas"} {
		tool, err := lookPath(name)
		if err != nil {
			continue
		}
		arguments := []string{}
		if !interactive {
			arguments = append(arguments, "-n")
		}
		arguments = append(arguments, "--", executable)
		arguments = append(arguments, args...)
		if err := run(ctx, tool, arguments); err != nil {
			return fmt.Errorf("%s: %w; run manually in a terminal: %s", name, err, ShellCommand(tool, append([]string{"--", executable}, publicUpdateArgs(args)...)...))
		}
		return nil
	}
	return fmt.Errorf("sudo or doas is required; run as administrator: %s", ShellCommand(executable, publicUpdateArgs(args)...))
}
