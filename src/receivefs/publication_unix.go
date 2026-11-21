//go:build !windows

package receivefs

import (
	"errors"
	"syscall"
)

func publicationUnsupported(err error) bool {
	// The probe links a private, owned regular file into a private directory.
	// EPERM and EACCES therefore also mean this filesystem or policy forbids
	// the primitive. Android shared storage, for example, rejects link(2)
	// with EACCES.
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS)
}
