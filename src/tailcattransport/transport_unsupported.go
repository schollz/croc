//go:build dragonfly || netbsd

package tailcattransport

import (
	"context"
	"net"

	"tailscale.com/types/key"
)

func Available() bool { return false }

func DerivePublicKeys([]byte) (key.NodePublic, key.NodePublic, error) {
	return key.NodePublic{}, key.NodePublic{}, ErrUnsupported
}

type Listener struct{}

func Listen(context.Context, []byte, Config, PathEvent) (*Listener, error) {
	return nil, ErrUnsupported
}

func (l *Listener) Offer() string                           { return "" }
func (l *Listener) Accept(context.Context) (*Bundle, error) { return nil, ErrUnsupported }
func (l *Listener) Close() error                            { return nil }

func ValidateOffer(string, []byte) error { return ErrUnsupported }

func Dial(context.Context, string, []byte, Config, PathEvent) (*Bundle, error) {
	return nil, ErrUnsupported
}

type Bundle struct{}

func (b *Bundle) Connections() []net.Conn { return nil }
func (b *Bundle) Stats() BundleStats      { return BundleStats{} }
func (b *Bundle) Close() error            { return nil }
func IsExpectedClose(error) bool          { return true }
