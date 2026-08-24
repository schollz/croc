//go:build linux && (386 || arm)

package derptransport

import (
	"context"
	"net"
)

// Available reports whether the derphole session implementation is compiled in.
func Available() bool { return false }

// Listen is unavailable on Linux 386 and ARM because derphole's packet session
// implementation does not compile on those targets.
func Listen(context.Context, PathEvent) (Listener, error) {
	return nil, ErrUnsupported
}

// Dial is unavailable on Linux 386 and ARM.
func Dial(context.Context, string, PathEvent) (net.Conn, error) {
	return nil, ErrUnsupported
}
