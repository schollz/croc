//go:build !windows

package store

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type rootLock struct {
	file *os.File
}

func acquireRootLock(root string) (*rootLock, error) {
	file, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open stored-transfer lock: %w", err)
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errorsNewStoreLocked()
	}
	return &rootLock{file: file}, nil
}

func (l *rootLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	return l.file.Close()
}

func errorsNewStoreLocked() error {
	return fmt.Errorf("stored-transfer directory is already in use")
}
