package webtty

import (
	"errors"
)

var (
	// ErrSlaveClosed indicates the function has exited by the slave
	ErrSlaveClosed = errors.New("slave closed")

	// ErrSlaveClosed is returned when the slave connection is closed.
	ErrMasterClosed = errors.New("master closed")
)
