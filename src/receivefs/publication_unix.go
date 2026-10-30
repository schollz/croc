//go:build !windows

package receivefs

import (
	"errors"
	"syscall"
)

func publicationUnsupported(err error) bool {
	// The probe links a private, owned regular file into a private directory.
	// EPERM therefore also means this filesystem or policy forbids the primitive.
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS)
}
