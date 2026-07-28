//go:build windows

package store

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type rootLock struct {
	file *os.File
}

func acquireRootLock(root string) (*rootLock, error) {
	file, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open stored-transfer lock: %w", err)
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stored-transfer directory is already in use")
	}
	return &rootLock{file: file}, nil
}

func (l *rootLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &overlapped)
	return l.file.Close()
}
