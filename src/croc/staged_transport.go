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

func (c *Client) openRelayChannels(attempt *transferAttemptState, indices []int, receive bool) error {
	if c.relayDataOpen != nil {
		return c.relayDataOpen(attempt, indices, receive)
	}
	return c.openRelayDataChannels(attempt, indices, receive)
}

func (c *Client) activateStagedTransportSender(attempt *transferAttemptState) error {
	baseCtx, baseCancel, _ := attempt.beginTailcatSetup(c.stop.ctx)
	setupCtx, timeoutCancel := context.WithTimeout(baseCtx, c.stagedSelectionTimeout())
	cancel := func() {
		timeoutCancel()
		baseCancel()
	}
	relayTimer := time.NewTimer(c.stagedRelayDelay())
	var relayTimerC <-chan time.Time = relayTimer.C

	var listener tailcatDataListener
	var derpBackup *tailcatDataBundle
	defer func() {
		relayTimer.Stop()
		if c.selectedDataTransport.Load() != selectedTransportUnset {
			return
		}
		if derpBackup != nil {
			_ = derpBackup.Close()
		}
		if listener != nil {
			_ = listener.Close()
		}
		cancel()
	}()

	listenResult := make(chan tailcatListenResult)
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
	var listenResultC <-chan tailcatListenResult = listenResult
	var acceptResultC <-chan tailcatDialResult
	var setupDoneC <-chan struct{} = setupCtx.Done()
	var relayResultC <-chan error
	var tokenValue string
	relayStarted := false
	relayFailed := false
	tailcatFailed := false
	var relaySetupErr error
	var tailcatSetupErr error

	stopRelayTimer := func() {
		if relayTimerC == nil {
			return
		}
		if !relayTimer.Stop() {
			select {
			case <-relayTimer.C:
			default:
			}
		}
		relayTimerC = nil
	}
	startRelay := func() error {
		if relayStarted {
			return nil
		}
		stopRelayTimer()
		relayStarted = true
		if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeRelayStandby}); err != nil {
			return err
		}
		relayResult := make(chan error, 1)
		relayResultC = relayResult
		go func() {
			relayResult <- c.openRelayChannels(attempt, stagedRelayIndices(len(c.Options.RelayPorts)), false)
		}()
		return nil
	}
	failTailcat := func(stage string, setupErr error) error {
		if tailcatFailed {
			return nil
		}
		tailcatFailed = true
		tailcatSetupErr = c.tailcatError(stage, setupErr, tokenValue)
		listenResultC = nil
		acceptResultC = nil
		setupDoneC = nil
		if listener != nil {
			_ = listener.Close()
			listener = nil
		}
		cancel()
		if !relayStarted {
			if err := startRelay(); err != nil {
				return c.tailcatError("relay standby", err, tokenValue)
			}
		}
		return nil
	}
	stagedFailure := func() error {
		return fmt.Errorf("%w: Tailcat setup failed: %v; relay standby failed: %v", ErrRelayConnection, tailcatSetupErr, relaySetupErr)
	}

	for {
		select {
		case result := <-listenResultC:
			listenResultC = nil
			if result.err != nil {
				if err := failTailcat("listener setup", result.err); err != nil {
					return err
				}
				if relayFailed {
					return stagedFailure()
				}
				continue
			}
			if result.listener == nil {
				if err := failTailcat("listener setup", errors.New("empty listener")); err != nil {
					return err
				}
				if relayFailed {
					return stagedFailure()
				}
				continue
			}
			listener = result.listener
			tokenValue = listener.Offer()
			if err := c.validateTailcatOffer(tokenValue); err != nil {
				if failErr := failTailcat("offer creation", err); failErr != nil {
					return failErr
				}
				if relayFailed {
					return stagedFailure()
				}
				continue
			}
			if err := message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeTailcatOffer, Message: tokenValue}); err != nil {
				return c.tailcatError("offer exchange", err, tokenValue)
			}
			acceptResult := make(chan tailcatDialResult)
			acceptResultC = acceptResult
			go func(listener tailcatDataListener) {
				bundle, err := listener.Accept(setupCtx)
				select {
				case acceptResult <- tailcatDialResult{bundle: bundle, err: err}:
				case <-setupCtx.Done():
					if bundle != nil {
						_ = bundle.Close()
					}
				}
			}(listener)
		case result := <-acceptResultC:
			acceptResultC = nil
			bundleErr := validateTailcatBundle(result.bundle)
			if result.err != nil || bundleErr != nil {
				if result.bundle != nil {
					_ = result.bundle.Close()
				}
				setupErr := result.err
				if setupErr == nil {
					setupErr = bundleErr
				}
				if err := failTailcat("peer connection", setupErr); err != nil {
					return err
				}
				if relayFailed {
					return stagedFailure()
				}
				continue
			}
			if tailcatBundlePath(result.bundle) != "derp" || relayFailed {
				return c.commitStagedTailcatSender(result.bundle, listener, cancel, tokenValue, attempt)
			}
			derpBackup = result.bundle
		case <-relayTimerC:
			relayTimerC = nil
			if err := startRelay(); err != nil {
				return c.tailcatError("relay standby", err, tokenValue)
			}
		case relayErr := <-relayResultC:
			relayResultC = nil
			if relayErr == nil {
				if derpBackup != nil {
					_ = derpBackup.Close()
					derpBackup = nil
				}
				return c.commitStagedRelaySender(listener, cancel, tokenValue, attempt)
			}
			relayFailed = true
			relaySetupErr = relayErr
			if derpBackup != nil {
				return c.commitStagedTailcatSender(derpBackup, listener, cancel, tokenValue, attempt)
			}
			if tailcatFailed {
				return stagedFailure()
			}
		case <-setupDoneC:
			setupDoneC = nil
			if c.stop.ctx.Err() != nil {
				return c.tailcatError("transport selection", c.stop.ctx.Err(), tokenValue)
			}
			if derpBackup != nil {
				return c.commitStagedTailcatSender(derpBackup, listener, cancel, tokenValue, attempt)
			}
			if err := failTailcat("transport selection", setupCtx.Err()); err != nil {
				return err
			}
			if relayFailed {
				return stagedFailure()
			}
		case <-c.stop.ctx.Done():
			return c.tailcatError("transport selection", c.stop.ctx.Err(), tokenValue)
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
	if listener != nil {
		_ = listener.Close()
	}
	c.cancelTailcatPreparation()
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
	if err := c.openRelayChannels(attempt, stagedRelayIndices(len(c.Options.RelayPorts)), false); err != nil {
		return err
	}
	c.relayStandbyMu.Lock()
	c.relayStandbyReady = true
	c.relayStandbyMu.Unlock()
	return nil
}

func (c *Client) relayStandbyIsReady() bool {
	c.relayStandbyMu.Lock()
	defer c.relayStandbyMu.Unlock()
	return c.relayStandbyReady
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
		if err := c.openRelayChannels(attempt, indices, !c.Options.IsSender); err != nil {
			attempt.report(err)
		}
	}()
}

func (c *Client) commitStagedRelayReceiver(attempt *transferAttemptState) error {
	if !c.relayStandbyIsReady() {
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
