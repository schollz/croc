package croc

import (
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
)

func stagedRelayIndices(portCount int) []int {
	count := min(portCount, 2, 8)
	indices := make([]int, count)
	for i := range indices {
		indices[i] = i
	}
	return indices
}

func relayRampIndices(portCount int) []int {
	limit := min(portCount, 8)
	if limit <= 2 {
		return nil
	}
	indices := make([]int, limit-2)
	for i := range indices {
		indices[i] = i + 2
	}
	return indices
}

func tailcatBundlePath(bundle *tailcatDataBundle) string {
	if bundle == nil || bundle.stats == nil {
		return ""
	}
	return bundle.stats().Path
}

func (c *Client) stagedRelayDelay() time.Duration {
	if c.stagedRelayDelayOverride > 0 {
		return c.stagedRelayDelayOverride
	}
	return stagedRelayDelay
}

func (c *Client) stagedSelectionTimeout() time.Duration {
	if c.stagedSelectionOverride > 0 {
		return c.stagedSelectionOverride
	}
	return stagedSelectionTimeout
}

func (c *Client) openStagedRelay(attempt *transferAttemptState, indices []int, receive bool) error {
	if c.stagedRelayOpen != nil {
		return c.stagedRelayOpen(attempt, indices, receive)
	}
	return c.openRelayDataChannels(attempt, indices, receive)
}

func (c *Client) activateStagedTransportSender(attempt *transferAttemptState) error {
	baseCtx, baseCancel, _ := attempt.beginTailcatSetup(c.stop.ctx)
	setupCtx, cancel := context.WithTimeout(baseCtx, c.stagedSelectionTimeout())
	defer func() {
		if c.selectedDataTransport.Load() == selectedTransportUnset {
			cancel()
			baseCancel()
		}
	}()

	listenResult := make(chan tailcatListenResult, 1)
	go func() {
		listener, err := c.listenTailcatData(setupCtx)
		select {
		case listenResult <- tailcatListenResult{listener: listener, err: err}:
		case <-setupCtx.Done():
			if listener != nil {
				_ = listener.Close()
			}
		}
	}()
	var listener tailcatDataListener
	select {
	case result := <-listenResult:
		if result.err != nil {
			return c.selectRelayAfterTailcatFailure("listener setup", result.err, "", attempt)
		}
		listener = result.listener
	case <-setupCtx.Done():
		return c.selectRelayAfterTailcatFailure("listener setup", setupCtx.Err(), "", attempt)
	}
	if listener == nil {
		return c.selectRelayAfterTailcatFailure("listener setup", errors.New("empty listener"), "", attempt)
	}
	tokenValue := listener.Offer()
	if err := c.validateTailcatOffer(tokenValue); err != nil {
		_ = listener.Close()
		return c.selectRelayAfterTailcatFailure("offer creation", err, tokenValue, attempt)
	}
	if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeTailcatOffer, Message: tokenValue}); err != nil {
		_ = listener.Close()
		return c.tailcatError("offer exchange", err, tokenValue)
	}

	acceptResult := make(chan tailcatDialResult, 1)
	go func() {
		bundle, err := listener.Accept(setupCtx)
		select {
		case acceptResult <- tailcatDialResult{bundle: bundle, err: err}:
		case <-setupCtx.Done():
			if bundle != nil {
				_ = bundle.Close()
			}
		}
	}()

	relayTimer := time.NewTimer(c.stagedRelayDelay())
	defer relayTimer.Stop()
	var relayTimerC <-chan time.Time = relayTimer.C
	relayResult := make(chan error, 1)
	relayStarted := false
	startRelay := func() error {
		if relayStarted {
			return nil
		}
		relayStarted = true
		if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeRelayStandby}); err != nil {
			return err
		}
		go func() {
			relayResult <- c.openStagedRelay(attempt, stagedRelayIndices(len(c.Options.RelayPorts)), false)
		}()
		return nil
	}

	var derpBackup *tailcatDataBundle
	tailcatFailed := false
	relayFailed := false
	for {
		select {
		case result := <-acceptResult:
			if result.err != nil || validateTailcatBundle(result.bundle) != nil {
				tailcatFailed = true
				if !relayStarted {
					relayTimerC = nil
					if err := startRelay(); err != nil {
						return c.tailcatError("relay standby", err, tokenValue)
					}
				}
				if relayFailed {
					_ = listener.Close()
					return c.tailcatError("staged transport", errors.New("Tailcat and relay setup failed"), tokenValue)
				}
				continue
			}
			if tailcatBundlePath(result.bundle) != "derp" || relayFailed {
				return c.commitStagedTailcatSender(result.bundle, listener, cancel, tokenValue, attempt)
			}
			derpBackup = result.bundle
			if !relayStarted {
				continue
			}
		case <-relayTimerC:
			relayTimerC = nil
			if err := startRelay(); err != nil {
				return c.tailcatError("relay standby", err, tokenValue)
			}
		case relayErr := <-relayResult:
			if relayErr == nil {
				if derpBackup != nil {
					_ = derpBackup.Close()
				}
				return c.commitStagedRelaySender(listener, cancel, tokenValue, attempt)
			}
			relayFailed = true
			if derpBackup != nil {
				return c.commitStagedTailcatSender(derpBackup, listener, cancel, tokenValue, attempt)
			}
			if tailcatFailed {
				_ = listener.Close()
				return fmt.Errorf("%w: relay standby failed: %v", ErrRelayConnection, relayErr)
			}
		case <-setupCtx.Done():
			if derpBackup != nil {
				return c.commitStagedTailcatSender(derpBackup, listener, cancel, tokenValue, attempt)
			}
			_ = listener.Close()
			return c.tailcatError("transport selection", setupCtx.Err(), tokenValue)
		}
	}
}

func (c *Client) commitStagedTailcatSender(bundle *tailcatDataBundle, listener tailcatDataListener, cancel context.CancelFunc, tokenValue string, attempt *transferAttemptState) error {
	if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeTransportSelect, Message: string(TransportDERP)}); err != nil {
		_ = bundle.Close()
		_ = listener.Close()
		return c.tailcatError("transport selection", err, tokenValue)
	}
	cleanup := func() {
		_ = listener.Close()
		cancel()
	}
	if err := c.installTailcatBundle(bundle, cleanup, attempt); err != nil {
		return err
	}
	log.Debug("selected staged Tailcat data transport")
	return c.finishDataTransportActivation()
}

func (c *Client) commitStagedRelaySender(listener tailcatDataListener, cancel context.CancelFunc, tokenValue string, attempt *transferAttemptState) error {
	if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeTransportSelect, Message: string(TransportRelay)}); err != nil {
		return c.tailcatError("transport selection", err, tokenValue)
	}
	_ = listener.Close()
	cancel()
	c.selectedDataTransport.Store(selectedTransportRelay)
	attempt.finishTailcatSetup()
	if err := c.finishDataTransportActivation(); err != nil {
		return err
	}
	if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeRelayRamp}); err != nil {
		return err
	}
	c.startRelayRamp(attempt)
	log.Debug("selected staged croc relay data transport")
	return nil
}

func (c *Client) processRelayStandby(attempt *transferAttemptState) error {
	if c.Options.IsSender || !c.peerStagedTransport || !c.tailcat.peerCapable {
		return errors.New("unexpected relay standby request")
	}
	if err := c.openRelayDataChannels(attempt, stagedRelayIndices(len(c.Options.RelayPorts)), false); err != nil {
		return err
	}
	c.relayStandbyMu.Lock()
	c.relayStandbyReady = true
	c.relayStandbyMu.Unlock()
	return nil
}

func (c *Client) processRelayRamp(attempt *transferAttemptState) error {
	if c.Options.IsSender || !c.peerStagedTransport || c.selectedDataTransport.Load() != selectedTransportRelay {
		return errors.New("unexpected relay ramp request")
	}
	c.startRelayRamp(attempt)
	return nil
}

func (c *Client) startRelayRamp(attempt *transferAttemptState) {
	indices := relayRampIndices(len(c.Options.RelayPorts))
	if len(indices) == 0 {
		return
	}
	go func() {
		if err := c.openStagedRelay(attempt, indices, !c.Options.IsSender); err != nil {
			attempt.report(err)
		}
	}()
}

func (c *Client) commitStagedRelayReceiver(attempt *transferAttemptState) error {
	c.relayStandbyMu.Lock()
	ready := c.relayStandbyReady
	c.relayStandbyMu.Unlock()
	if !ready {
		return errors.New("relay selected without ready standby channels")
	}
	attempt.closeTailcatPending()
	attempt.cancelTailcatSetup()
	c.selectedDataTransport.Store(selectedTransportRelay)
	for i, connection := range c.connectionsSnapshot()[1:] {
		if connection != nil {
			go c.receiveData(i, connection, attempt)
		}
	}
	attempt.finishTailcatSetup()
	return c.finishDataTransportActivation()
}
