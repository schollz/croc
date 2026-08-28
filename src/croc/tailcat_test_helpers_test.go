package croc

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/tailcattransport"
)

type fakeTailcatTransport struct {
	available bool
	listen    func(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error)
	dial      func(context.Context, string, []byte, tailcattransport.PathEvent) (*tailcatDataBundle, error)
	validate  func(string, []byte) error
}

type fakeTailcatListener struct {
	offer     string
	accept    func(context.Context) (*tailcatDataBundle, error)
	closed    atomic.Bool
	closeOnce sync.Once
}

func (l *fakeTailcatListener) Offer() string { return l.offer }

func (l *fakeTailcatListener) Accept(ctx context.Context) (*tailcatDataBundle, error) {
	if l.accept == nil {
		return nil, errors.New("fake Tailcat accept is not configured")
	}
	return l.accept(ctx)
}

func (l *fakeTailcatListener) Close() error {
	l.closeOnce.Do(func() { l.closed.Store(true) })
	return nil
}

func newTailcatTestAttempt(control *comm.Comm) *transferAttemptState {
	return &transferAttemptState{
		errc:    make(chan error, 1),
		control: control,
		tailcat: tailcatAttemptState{setupDone: make(chan struct{})},
	}
}

func newPipeDataBundles(streams int) (sender, receiver *tailcatDataBundle) {
	senderConnections := make([]net.Conn, streams)
	receiverConnections := make([]net.Conn, streams)
	for i := range streams {
		senderConnections[i], receiverConnections[i] = net.Pipe()
	}
	newBundle := func(connections []net.Conn) *tailcatDataBundle {
		return &tailcatDataBundle{
			connections: connections,
			stats: func() tailcattransport.BundleStats {
				return tailcattransport.BundleStats{Path: "direct", StreamCount: streams}
			},
			cleanup: func() error {
				for _, conn := range connections {
					_ = conn.Close()
				}
				return nil
			},
		}
	}
	return newBundle(senderConnections), newBundle(receiverConnections)
}

func (f *fakeTailcatTransport) Available() bool { return f.available }

func (f *fakeTailcatTransport) Listen(ctx context.Context, key []byte, events tailcattransport.PathEvent) (tailcatDataListener, error) {
	if f.listen == nil {
		return nil, errors.New("fake Tailcat listener is not configured")
	}
	return f.listen(ctx, key, events)
}

func (f *fakeTailcatTransport) Dial(ctx context.Context, offer string, key []byte, events tailcattransport.PathEvent) (*tailcatDataBundle, error) {
	if f.dial == nil {
		return nil, errors.New("fake Tailcat dialer is not configured")
	}
	return f.dial(ctx, offer, key, events)
}

func (f *fakeTailcatTransport) ValidateOffer(offer string, key []byte) error {
	if f.validate == nil {
		return nil
	}
	return f.validate(offer, key)
}

type unavailableTestDataTransport struct{}

func (*unavailableTestDataTransport) Available() bool { return false }

func (*unavailableTestDataTransport) Listen(context.Context, []byte, tailcattransport.PathEvent) (tailcatDataListener, error) {
	return nil, tailcattransport.ErrUnsupported
}

func (*unavailableTestDataTransport) Dial(context.Context, string, []byte, tailcattransport.PathEvent) (*tailcatDataBundle, error) {
	return nil, tailcattransport.ErrUnsupported
}

func (*unavailableTestDataTransport) ValidateOffer(string, []byte) error {
	return tailcattransport.ErrUnsupported
}

func newTestClient(ops Options) (*Client, error) {
	return newClient(ops, &unavailableTestDataTransport{})
}
