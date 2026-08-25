//go:build linux && (386 || arm)

package derptransport

import (
	"context"
	"net"
)

// Available reports whether the derphole session implementation is compiled in.
func Available() bool { return false }

func IsCleanGroupClose(error) bool { return false }

// Listen is unavailable on Linux 386 and ARM because derphole's packet session
// implementation does not compile on those targets.
func Listen(context.Context, PathEvent) (Listener, error) {
	return nil, ErrUnsupported
}

// Dial is unavailable on Linux 386 and ARM.
func Dial(context.Context, string, PathEvent) (net.Conn, error) {
	return nil, ErrUnsupported
}

func ListenGroup(context.Context, PathEvent) (GroupListener, error) {
	return nil, ErrUnsupported
}

func ListenGroupWithConfig(context.Context, PathEvent, GroupConfig) (GroupListener, error) {
	return nil, ErrUnsupported
}

func DialGroup(context.Context, string, PathEvent) (Bundle, error) {
	return nil, ErrUnsupported
}

func DialGroupWithConfig(context.Context, string, PathEvent, GroupConfig) (Bundle, error) {
	return nil, ErrUnsupported
}
