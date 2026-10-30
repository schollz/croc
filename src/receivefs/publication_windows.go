package receivefs

import (
	"errors"
	"syscall"
)

func publicationUnsupported(err error) bool {
	// ERROR_INVALID_FUNCTION, ERROR_NOT_SAME_DEVICE, ERROR_NOT_SUPPORTED.
	return errors.Is(err, syscall.Errno(1)) || errors.Is(err, syscall.Errno(17)) || errors.Is(err, syscall.Errno(50))
}
