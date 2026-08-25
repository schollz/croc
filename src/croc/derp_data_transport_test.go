package croc

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/schollz/croc/v11/src/derptransport"
)

type fakeDERPDataTransport struct {
	available          bool
	attachGroupEnabled bool
	listen             func(context.Context, derptransport.PathEvent) (derptransport.Listener, error)
	dial               func(context.Context, string, derptransport.PathEvent) (net.Conn, error)
	listenGroup        func(context.Context, derptransport.PathEvent) (derptransport.GroupListener, error)
	dialGroup          func(context.Context, string, derptransport.PathEvent) (derptransport.Bundle, error)
}

func newFakeDERPDataTransport(grouped bool) *fakeDERPDataTransport {
	return &fakeDERPDataTransport{available: true, attachGroupEnabled: grouped}
}

func (f *fakeDERPDataTransport) Available() bool {
	return f != nil && f.available
}

func (f *fakeDERPDataTransport) AttachGroupEnabled() bool {
	return f != nil && f.attachGroupEnabled
}

func (f *fakeDERPDataTransport) Listen(ctx context.Context, events derptransport.PathEvent, grouped bool) (derpDataListener, error) {
	if grouped {
		if f == nil || f.listenGroup == nil {
			return nil, errors.New("test AttachGroup listener is not configured")
		}
		listener, err := f.listenGroup(ctx, events)
		if err != nil {
			return nil, err
		}
		return groupDERPDataListener{GroupListener: listener}, nil
	}
	if f == nil || f.listen == nil {
		return nil, errors.New("test DERP listener is not configured")
	}
	listener, err := f.listen(ctx, events)
	if err != nil {
		return nil, err
	}
	return singleDERPDataListener{Listener: listener}, nil
}

func (f *fakeDERPDataTransport) Dial(ctx context.Context, tokenValue string, events derptransport.PathEvent, grouped bool) (*derpDataBundle, error) {
	if grouped {
		if f == nil || f.dialGroup == nil {
			return nil, errors.New("test AttachGroup dialer is not configured")
		}
		bundle, err := f.dialGroup(ctx, tokenValue, events)
		if err != nil {
			return nil, err
		}
		return newGroupDERPBundle(bundle), nil
	}
	if f == nil || f.dial == nil {
		return nil, errors.New("test DERP dialer is not configured")
	}
	conn, err := f.dial(ctx, tokenValue, events)
	if err != nil {
		return nil, err
	}
	return newSingleDERPBundle(conn), nil
}

func (*fakeDERPDataTransport) ValidateToken(tokenValue string, now time.Time, grouped bool) error {
	return (productionDERPDataTransport{}).ValidateToken(tokenValue, now, grouped)
}

func newTestClient(ops Options) (*Client, error) {
	return newClient(ops, &fakeDERPDataTransport{})
}
