package croc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/peerdiscovery"
)

const (
	relayHappyEyeballsDelay = 50 * time.Millisecond
	relayDiscoveryStagger   = 75 * time.Millisecond
)

type rawRelayDialResult struct {
	connection *comm.Comm
	address    string
	err        error
}

type relayControlResult struct {
	connection *comm.Comm
	address    string
	banner     string
	externalIP string
	capability string
}

type receiverRelayAttempt struct {
	result relayControlResult
	local  bool
	err    error
}

func raceRelayTCP(ctx context.Context, addresses []string, timeout, stagger time.Duration) (*comm.Comm, string, error) {
	unique := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = normalizeRelayAddress(address)
		if address == "" {
			continue
		}
		seen := slices.Contains(unique, address)
		if !seen {
			unique = append(unique, address)
		}
	}
	if len(unique) == 0 {
		return nil, "", errors.New("found no addresses to connect")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan rawRelayDialResult)
	for i, address := range unique {
		delay := time.Duration(i) * stagger
		go func(address string, delay time.Duration) {
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
			}
			connection, err := comm.NewConnection(address, timeout)
			result := rawRelayDialResult{connection: connection, address: address, err: err}
			select {
			case results <- result:
			case <-ctx.Done():
				if connection != nil {
					connection.Close()
				}
			}
		}(address, delay)
	}

	var failures []error
	for range unique {
		select {
		case result := <-results:
			if result.err == nil && result.connection != nil {
				cancel()
				return result.connection, result.address, nil
			}
			failures = append(failures, fmt.Errorf("%s: %w", result.address, result.err))
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	return nil, "", errors.Join(failures...)
}

func (c *Client) connectRelayControl(addresses ...string) (relayControlResult, error) {
	ctx := context.Background()
	if c != nil && c.stop != nil && c.stop.ctx != nil {
		ctx = c.stop.ctx
	}
	return c.connectRelayControlContext(ctx, addresses...)
}

func (c *Client) connectRelayControlContext(ctx context.Context, addresses ...string) (relayControlResult, error) {
	connection, address, err := raceRelayTCP(ctx, addresses, 5*time.Second, relayHappyEyeballsDelay)
	if err != nil {
		return relayControlResult{}, err
	}
	stopClose := context.AfterFunc(ctx, connection.Close)
	banner, externalIP, capability, err := tcp.HandshakeTCPServerCapability(connection, c.Options.RelayPassword, c.Options.RoomName)
	closeStopped := stopClose()
	if err != nil {
		connection.Close()
		return relayControlResult{}, err
	}
	if !closeStopped || ctx.Err() != nil {
		connection.Close()
		return relayControlResult{}, ctx.Err()
	}
	return relayControlResult{
		connection: connection,
		address:    address,
		banner:     banner,
		externalIP: externalIP,
		capability: capability,
	}, nil
}

func discoveredRelayAddresses(discoveries []peerdiscovery.Discovered) []string {
	var addresses []string
	for _, discovery := range discoveries {
		if !bytes.HasPrefix(discovery.Payload, []byte("croc")) {
			continue
		}
		port := string(bytes.TrimPrefix(discovery.Payload, []byte("croc")))
		if port == "" {
			port = models.DEFAULT_PORT
		}
		addresses = append(addresses, net.JoinHostPort(discovery.Address, port))
	}
	return addresses
}

// connectReceiverRelayControl overlaps LAN discovery with the public relay
// dial. Only the winning raw address joins a room within each route attempt,
// and every losing authenticated connection is closed.
func (c *Client) connectReceiverRelayControl(publicAddresses ...string) (relayControlResult, bool, error) {
	routeCtx, cancel := context.WithCancel(c.stop.ctx)
	defer cancel()
	attempts := make(chan receiverRelayAttempt)
	discoveryResults := make(chan []peerdiscovery.Discovered, 1)

	go func() {
		discoveries := c.discoverReceivePeers()
		select {
		case discoveryResults <- discoveries:
		case <-routeCtx.Done():
		}
	}()

	publicPending := false
	for _, address := range publicAddresses {
		if normalizeRelayAddress(address) != "" {
			publicPending = true
			break
		}
	}
	if publicPending {
		go func() {
			timer := time.NewTimer(relayDiscoveryStagger)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-routeCtx.Done():
				return
			}
			result, err := c.connectRelayControlContext(routeCtx, publicAddresses...)
			attempt := receiverRelayAttempt{result: result, err: err}
			select {
			case attempts <- attempt:
			case <-routeCtx.Done():
				if result.connection != nil {
					result.connection.Close()
				}
			}
		}()
	}

	discoveryPending := true
	localPending := false
	var failures []error
	for discoveryPending || publicPending || localPending {
		select {
		case results := <-discoveryResults:
			discoveryPending = false
			addresses := discoveredRelayAddresses(results)
			if len(addresses) == 0 {
				continue
			}
			localPending = true
			go func() {
				result, err := c.connectRelayControlContext(routeCtx, addresses...)
				attempt := receiverRelayAttempt{result: result, local: true, err: err}
				select {
				case attempts <- attempt:
				case <-routeCtx.Done():
					if result.connection != nil {
						result.connection.Close()
					}
				}
			}()
		case attempt := <-attempts:
			if attempt.local {
				localPending = false
			} else {
				publicPending = false
			}
			if attempt.err != nil {
				failures = append(failures, attempt.err)
				continue
			}
			if !attempt.local && localPending {
				select {
				case localAttempt := <-attempts:
					if localAttempt.local {
						localPending = false
						if localAttempt.err == nil {
							attempt.result.connection.Close()
							return localAttempt.result, true, nil
						}
						failures = append(failures, localAttempt.err)
					} else if localAttempt.result.connection != nil {
						localAttempt.result.connection.Close()
					}
				default:
				}
			}
			return attempt.result, attempt.local, nil
		case <-routeCtx.Done():
			return relayControlResult{}, false, routeCtx.Err()
		}
	}
	if len(failures) == 0 {
		failures = append(failures, errors.New("found no relay addresses"))
	}
	return relayControlResult{}, false, errors.Join(failures...)
}
