package localcommand

import (
	"syscall"
	"time"
)

type Option func(*LocalCommand)

func WithCloseSignal(signal syscall.Signal) Option {
	return func(lcmd *LocalCommand) {
		lcmd.closeSignal = signal
	}
}

func WithCloseTimeout(timeout time.Duration) Option {
	return func(lcmd *LocalCommand) {
		lcmd.closeTimeout = timeout
	}
}
