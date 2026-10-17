package croc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
)

// Keep each bound wildcard socket open through handoff to the relay, closing
// the group if reservation fails.
func reserveLocalRelayListeners(ctx context.Context, firstPort, count int,
	listen func(context.Context, string, string) (net.Listener, error),
) ([]net.Listener, error) {
	if firstPort < 1 || firstPort > 65535 || count < 1 {
		return nil, errors.New("local relay needs a valid first port and positive listener count")
	}
	listeners := make([]net.Listener, 0, count)
	success := false
	defer func() {
		if !success {
			for _, listener := range listeners {
				listener.Close()
			}
		}
	}()
	var lastErr error
	for port := firstPort; port < min(firstPort+200, 65536); port++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		listener, err := listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(port)))
		if err != nil {
			lastErr = err
			continue
		}
		listeners = append(listeners, listener)
		if len(listeners) == count {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			success = true
			return listeners, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := fmt.Errorf("not enough open ports to run local relay: reserved %d of %d", len(listeners), count)
	if lastErr != nil {
		err = fmt.Errorf("%w: %w", err, lastErr)
	}
	return nil, err
}

func (c *Client) setupLocalRelay() error {
	if len(c.Options.RelayPorts) == 0 {
		return errors.New("local relay needs at least one port")
	}
	firstPort, err := strconv.Atoi(c.Options.RelayPorts[0])
	if err != nil {
		return fmt.Errorf("invalid local relay port: %w", err)
	}
	lc := net.ListenConfig{}
	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		// BSD systems can allow a wildcard listener to overlap an existing
		// loopback listener. Avoid selecting that port, where local clients
		// would reach the older listener instead of our relay.
		dialer := net.Dialer{Timeout: 100 * time.Millisecond}
		if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port)); err == nil {
			conn.Close()
			return nil, fmt.Errorf("local relay port %s already has a loopback listener", port)
		}
		return lc.Listen(ctx, network, address)
	}
	listeners, err := reserveLocalRelayListeners(c.stop.ctx, firstPort, len(c.Options.RelayPorts), listen)
	if err != nil {
		return err
	}
	relayCtx, cancelRelay := context.WithCancel(c.stop.ctx)
	closeListeners := sync.OnceFunc(func() {
		cancelRelay()
		for _, listener := range listeners {
			listener.Close()
		}
	})
	stopClose := context.AfterFunc(relayCtx, closeListeners)

	localRelayPorts := make([]string, len(listeners))
	for i, listener := range listeners {
		_, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			closeListeners()
			stopClose()
			return fmt.Errorf("invalid local relay address: %w", err)
		}
		localRelayPorts[i] = port
	}
	// Capture these before external relay negotiation can overwrite RelayPorts.
	c.Options.RelayPorts = localRelayPorts
	c.localRelayPort = localRelayPorts[0]
	localRelayBanner := strings.Join(localRelayPorts[1:], ",")
	debugLevel := "warn"
	if c.Options.Debug {
		debugLevel = "debug"
	}
	password := c.Options.RelayPassword
	var workers sync.WaitGroup
	for _, listener := range listeners {
		workers.Go(func() {
			defer closeListeners()
			if err := c.stop.run(relayCtx, debugLevel, listener, password, localRelayBanner); err != nil {
				log.Errorf("local relay stopped: %v", err)
			}
		})
	}
	go func() {
		workers.Wait()
		stopClose()
		closeListeners()
	}()
	return nil
}
