package croc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/redact"
)

const (
	tailcatFeature                  = "experimental-tailcat-v1"
	tailcatRequiredFeature          = "experimental-tailcat-required-v1"
	tailcatSetupTimeout             = 30 * time.Second
	tailcatStatusReady              = "ready"
	tailcatStatusFallback           = "fallback"
	selectedTransportTailcat        = 1
	autoTailcatThresholdBytes int64 = 128 * 1024 * 1024
	stagedRelayDelay                = 350 * time.Millisecond
	stagedSelectionTimeout          = 8 * time.Second
)

type tailcatClientState struct {
	transport           tailcatDataTransport
	peerCapable         bool
	peerRequired        bool
	offerReceived       bool
	transferBytes       atomic.Int64
	autoDecisionOnce    sync.Once
	transferConnections atomic.Int32
	terminal            atomic.Bool
	bundleMu            sync.Mutex
	bundle              *tailcatDataBundle
	prepareOnce         sync.Once
	prepareReady        chan struct{}
	prepared            any
	prepareErr          error
}

type tailcatAttemptState struct {
	setupDone     chan struct{}
	setupOnce     sync.Once
	setupMu       sync.Mutex
	setupContext  context.Context
	setupCancel   context.CancelFunc
	setupDeadline time.Time
	pendingMu     sync.Mutex
	pending       *tailcatDataBundle
	pendingCancel context.CancelFunc
	pendingReady  chan struct{}
	pendingOnce   sync.Once
	statusOnce    sync.Once
	statusErr     error
}

func (a *transferAttemptState) beginTailcatSetup(parent context.Context) (context.Context, context.CancelFunc, time.Time) {
	if a == nil {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, time.Now().Add(tailcatSetupTimeout)
	}
	a.tailcat.setupMu.Lock()
	defer a.tailcat.setupMu.Unlock()
	if a.tailcat.setupContext == nil {
		a.tailcat.setupContext, a.tailcat.setupCancel = context.WithCancel(parent)
		a.tailcat.setupDeadline = time.Now().Add(tailcatSetupTimeout)
	}
	return a.tailcat.setupContext, a.tailcat.setupCancel, a.tailcat.setupDeadline
}

func (a *transferAttemptState) setTailcatPending(bundle *tailcatDataBundle, cancel context.CancelFunc) error {
	if a == nil || bundle == nil {
		if bundle != nil {
			_ = bundle.Close()
		}
		if cancel != nil {
			cancel()
		}
		return errors.New("Tailcat returned an empty connection")
	}
	a.tailcat.pendingMu.Lock()
	defer a.tailcat.pendingMu.Unlock()
	if a.tailcat.pending != nil {
		_ = bundle.Close()
		if cancel != nil {
			cancel()
		}
		return errors.New("duplicate pending Tailcat connection")
	}
	a.tailcat.pending = bundle
	a.tailcat.pendingCancel = cancel
	ready := a.tailcatPendingReady()
	a.tailcat.pendingOnce.Do(func() { close(ready) })
	return nil
}

func (a *transferAttemptState) tailcatPendingReady() chan struct{} {
	a.tailcat.setupMu.Lock()
	defer a.tailcat.setupMu.Unlock()
	if a.tailcat.pendingReady == nil {
		a.tailcat.pendingReady = make(chan struct{})
	}
	return a.tailcat.pendingReady
}

func (a *transferAttemptState) takeTailcatPending() (*tailcatDataBundle, context.CancelFunc) {
	if a == nil {
		return nil, nil
	}
	a.tailcat.pendingMu.Lock()
	defer a.tailcat.pendingMu.Unlock()
	bundle, cancel := a.tailcat.pending, a.tailcat.pendingCancel
	a.tailcat.pending = nil
	a.tailcat.pendingCancel = nil
	return bundle, cancel
}

func (a *transferAttemptState) closeTailcatPending() {
	bundle, cancel := a.takeTailcatPending()
	if bundle != nil {
		_ = bundle.Close()
	}
	if cancel != nil {
		cancel()
	}
}

func (a *transferAttemptState) sendTailcatStatus(control *comm.Comm, key []byte, status string) error {
	if a == nil || control == nil {
		return errors.New("Tailcat status control connection is unavailable")
	}
	a.tailcat.statusOnce.Do(func() {
		a.tailcat.statusErr = message.Send(control, key, message.Message{
			Type:    message.TypeTailcatStatus,
			Message: status,
		})
	})
	return a.tailcat.statusErr
}

func (a *transferAttemptState) finishTailcatSetup() {
	if a == nil || a.tailcat.setupDone == nil {
		return
	}
	a.tailcat.setupOnce.Do(func() { close(a.tailcat.setupDone) })
}

func (a *transferAttemptState) cancelTailcatSetup() {
	if a == nil {
		return
	}
	a.tailcat.setupMu.Lock()
	cancel := a.tailcat.setupCancel
	a.tailcat.setupMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) activateSecureChannel(attempt *transferAttemptState) (err error) {
	log.Debug("PAKE key confirmation succeeded")
	if !c.Options.IsSender {
		c.setReceiveStatus(receiveStatusOpeningTransferChannels)
	}
	localTailcat := c.localTailcatSupported()
	if !c.Options.IsSender && c.peerRequiresTailcat() && !localTailcat {
		return fmt.Errorf("peer requires Tailcat, but Tailcat is disabled or unavailable locally")
	}
	if c.Options.Transport == TransportDERP && !c.tailcat.peerCapable {
		return fmt.Errorf("--transport derp requires a Tailcat-capable peer")
	}
	if localTailcat && c.tailcat.peerCapable {
		if c.Options.IsSender {
			if c.Options.Transport == TransportAuto && c.peerStagedTransport {
				return c.activateStagedTransportSender(attempt)
			}
			return c.activateTailcatSender(attempt)
		}
		c.watchTailcatSelection(attempt)
		return nil
	}
	log.Debug("selected croc relay data transport")
	return c.activateRelayDataChannels(attempt)
}

func (c *Client) pakeFeatures() []string {
	features := []string{inlinePeerMetadataFeature, progressiveFileHashFeature, stagedTransportFeature, implicitTailcatReadyFeature}
	if !c.localTailcatSupported() {
		return features
	}
	features = append(features, tailcatFeature)
	if c.Options.IsSender && c.Options.Transport == TransportDERP {
		features = append(features, tailcatRequiredFeature)
	}
	return features
}

func (c *Client) localTailcatSupported() bool {
	if c.Options.Transport == TransportRelay || c.Options.OnlyLocal || !c.dataTransport().Available() {
		return false
	}
	if !c.Options.IsSender || c.Options.Transport == TransportDERP {
		return true
	}
	return c.autoTailcatEligible()
}

func (c *Client) tailcatFallbackAllowed() bool {
	if c.Options.IsSender {
		return c.Options.Transport == TransportAuto
	}
	return !c.peerRequiresTailcat()
}

func (c *Client) autoTailcatEligible() bool {
	transferBytes := c.tailcat.transferBytes.Load()
	eligible := transferBytes >= autoTailcatThresholdBytes
	c.tailcat.autoDecisionOnce.Do(func() {
		selected := TransportRelay
		if eligible {
			selected = TransportDERP
		}
		log.Debugf(
			"auto transport size decision: bytes=%d threshold=%d selected=%s",
			transferBytes,
			autoTailcatThresholdBytes,
			selected,
		)
	})
	return eligible
}

func totalLogicalTransferSize(files []FileInfo) int64 {
	var total int64
	for _, file := range files {
		if file.Size <= 0 {
			continue
		}
		if file.Size > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += file.Size
	}
	return total
}

func (c *Client) listenTailcatData(ctx context.Context) (tailcatDataListener, error) {
	if c.tailcat.prepareReady != nil {
		select {
		case <-c.tailcat.prepareReady:
			if c.tailcat.prepareErr != nil {
				return nil, c.tailcat.prepareErr
			}
			if transport, ok := c.dataTransport().(tailcatPreparingTransport); ok {
				return transport.ListenPrepared(ctx, c.Key, c.tailcatPathEvent, c.tailcat.prepared)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.dataTransport().Listen(ctx, c.Key, c.tailcatPathEvent)
}

func (c *Client) startTailcatPreparation() {
	transport, ok := c.dataTransport().(tailcatPreparingTransport)
	if !ok || !c.dataTransport().Available() || c.Options.Transport == TransportRelay || c.Options.OnlyLocal {
		return
	}
	if c.Options.Transport == TransportAuto && c.tailcat.transferBytes.Load() < 128*1024*1024 {
		return
	}
	c.tailcat.prepareOnce.Do(func() {
		c.tailcat.prepareReady = make(chan struct{})
		go func() {
			defer close(c.tailcat.prepareReady)
			c.tailcat.prepared, c.tailcat.prepareErr = transport.Prepare(c.stop.ctx)
			if c.tailcat.prepareErr != nil {
				log.Debugf("Tailcat preparation failed: %v", c.tailcat.prepareErr)
				return
			}
			log.Debug("Tailcat bootstrap region prepared")
		}()
	})
}

func (c *Client) dialTailcatData(ctx context.Context, offer string) (*tailcatDataBundle, error) {
	return c.dataTransport().Dial(ctx, offer, c.Key, c.tailcatPathEvent)
}

func (c *Client) validateTailcatOffer(offer string) error {
	return c.dataTransport().ValidateOffer(offer, c.Key)
}

func (c *Client) peerRequiresTailcat() bool {
	return c.tailcat.peerCapable && c.tailcat.peerRequired
}

func (c *Client) transferConnectionCount() int {
	if c.selectedDataTransport.Load() == selectedTransportTailcat {
		if count := int(c.tailcat.transferConnections.Load()); count > 0 {
			return count
		}
		return 1
	}
	return len(c.Options.RelayPorts)
}

func (c *Client) tailcatPathEvent(status string) {
	log.Debugf("Tailcat transport: %s", status)
}

func (c *Client) tailcatError(stage string, err error, tokenValue string) error {
	safeErr := redact.Error(err, tokenValue, c.Options.SharedSecret)
	log.Debugf("Tailcat %s failed: %v", stage, safeErr)
	return fmt.Errorf("%w: %s: %w", ErrDERPConnection, stage, safeErr)
}

func (c *Client) installTailcatBundle(bundle *tailcatDataBundle, cleanup func(), attempt *transferAttemptState) error {
	if err := validateTailcatBundle(bundle); err != nil {
		if bundle != nil {
			_ = bundle.Close()
		}
		if cleanup != nil {
			cleanup()
		}
		return err
	}
	bundle.addCleanup(cleanup)
	c.closeTailcatBundle()
	c.selectedDataTransport.Store(selectedTransportTailcat)
	c.replaceDataConnections(bundle.connections)
	c.tailcat.bundleMu.Lock()
	c.tailcat.bundle = bundle
	c.tailcat.bundleMu.Unlock()
	c.tailcat.terminal.Store(false)
	c.tailcat.transferConnections.Store(int32(len(bundle.connections)))
	attempt.finishTailcatSetup()
	if !c.Options.IsSender {
		for i := range bundle.connections {
			go c.receiveData(i, c.connection(i+1), attempt)
		}
	}
	return nil
}

func (c *Client) closeTailcatBundle() {
	c.tailcat.bundleMu.Lock()
	bundle := c.tailcat.bundle
	c.tailcat.bundle = nil
	c.tailcat.bundleMu.Unlock()
	if bundle != nil {
		_ = bundle.Close()
	}
	c.tailcat.transferConnections.Store(0)
}

func (c *Client) activateTailcatSender(attempt *transferAttemptState) error {
	setupCtx, cancel, deadline := attempt.beginTailcatSetup(c.stop.ctx)
	decisionDeadline := deadline.Add(-2 * time.Second)
	listenResult := make(chan tailcatListenResult)
	go func() {
		listener, err := c.listenTailcatData(setupCtx)
		result := tailcatListenResult{listener: listener, err: err}
		select {
		case listenResult <- result:
		case <-setupCtx.Done():
			if listener != nil {
				_ = listener.Close()
			}
		}
	}()

	var listener tailcatDataListener
	timer := time.NewTimer(max(time.Until(decisionDeadline), 0))
	select {
	case result := <-listenResult:
		timer.Stop()
		if result.err != nil {
			cancel()
			return c.selectRelayAfterTailcatFailure("listener setup", result.err, "", attempt)
		}
		if result.listener == nil {
			cancel()
			return c.selectRelayAfterTailcatFailure("listener setup", errors.New("empty listener"), "", attempt)
		}
		listener = result.listener
	case <-timer.C:
		cancel()
		return c.selectRelayAfterTailcatFailure("listener setup", context.DeadlineExceeded, "", attempt)
	case <-c.stop.ctx.Done():
		timer.Stop()
		cancel()
		return c.tailcatError("listener setup", c.stop.ctx.Err(), "")
	}

	tokenValue := listener.Offer()
	if err := c.validateTailcatOffer(tokenValue); err != nil {
		_ = listener.Close()
		cancel()
		return c.selectRelayAfterTailcatFailure("offer creation", err, tokenValue, attempt)
	}
	controlConn := c.connection(0).Connection()
	if err := controlConn.SetWriteDeadline(decisionDeadline); err != nil {
		_ = listener.Close()
		cancel()
		return c.tailcatError("offer exchange", err, tokenValue)
	}
	if err := message.Send(c.connection(0), c.Key, message.Message{
		Type:    message.TypeTailcatOffer,
		Message: tokenValue,
	}); err != nil {
		_ = listener.Close()
		cancel()
		return c.tailcatError("offer exchange", err, tokenValue)
	}
	if err := controlConn.SetWriteDeadline(time.Now().Add(3 * time.Hour)); err != nil {
		_ = listener.Close()
		cancel()
		return c.tailcatError("offer exchange", err, tokenValue)
	}

	acceptResult := make(chan tailcatDialResult)
	go func() {
		bundle, err := listener.Accept(setupCtx)
		result := tailcatDialResult{bundle: bundle, err: err}
		select {
		case acceptResult <- result:
		case <-setupCtx.Done():
			if bundle != nil {
				_ = bundle.Close()
			}
		}
	}()

	status, statusErr := c.receiveTailcatStatus(decisionDeadline)
	if statusErr != nil {
		_ = listener.Close()
		cancel()
		return c.selectRelayAfterTailcatFailure("peer readiness", statusErr, tokenValue, attempt)
	}
	if status == tailcatStatusFallback {
		_ = listener.Close()
		cancel()
		return c.selectRelayAfterTailcatFailure("peer connection", errors.New("peer could not establish Tailcat"), tokenValue, attempt)
	}
	if status != tailcatStatusReady {
		_ = listener.Close()
		cancel()
		return fmt.Errorf("invalid Tailcat status %q", status)
	}

	timer = time.NewTimer(max(time.Until(decisionDeadline), 0))
	select {
	case result := <-acceptResult:
		timer.Stop()
		if result.err != nil {
			_ = listener.Close()
			cancel()
			return c.selectRelayAfterTailcatFailure("peer connection", result.err, tokenValue, attempt)
		}
		if bundleErr := validateTailcatBundle(result.bundle); bundleErr != nil {
			_ = listener.Close()
			cancel()
			return c.selectRelayAfterTailcatFailure("peer connection", bundleErr, tokenValue, attempt)
		}
		if err := message.Send(c.connection(0), c.Key, message.Message{
			Type:    message.TypeTransportSelect,
			Message: string(TransportDERP),
		}); err != nil {
			_ = result.bundle.Close()
			_ = listener.Close()
			cancel()
			return c.tailcatError("transport selection", err, tokenValue)
		}
		log.Debug("selected Tailcat data transport")
		cleanup := func() {
			_ = listener.Close()
			cancel()
		}
		if err := c.installTailcatBundle(result.bundle, cleanup, attempt); err != nil {
			return err
		}
		return c.finishDataTransportActivation()
	case <-timer.C:
		_ = listener.Close()
		cancel()
		return c.selectRelayAfterTailcatFailure("peer connection", context.DeadlineExceeded, tokenValue, attempt)
	case <-c.stop.ctx.Done():
		timer.Stop()
		_ = listener.Close()
		cancel()
		return c.tailcatError("peer connection", c.stop.ctx.Err(), tokenValue)
	}
}

func (c *Client) receiveTailcatStatus(deadline time.Time) (string, error) {
	payload, err := c.connection(0).ReceiveWithDeadline(deadline)
	if err != nil {
		return "", err
	}
	status, err := message.Decode(c.Key, payload)
	if err != nil {
		return "", tailcatProtocolError{err: fmt.Errorf("invalid encrypted Tailcat status: %w", err)}
	}
	if status.Type == message.TypeError {
		return "", tailcatProtocolError{err: fmt.Errorf("peer error: %s", status.Message)}
	}
	if status.Type != message.TypeTailcatStatus {
		return "", tailcatProtocolError{err: fmt.Errorf("expected Tailcat status, got %s", status.Type)}
	}
	return status.Message, nil
}

func (c *Client) selectRelayAfterTailcatFailure(stage string, setupErr error, tokenValue string, attempt *transferAttemptState) error {
	tailcatErr := c.tailcatError(stage, setupErr, tokenValue)
	var protocolErr tailcatProtocolError
	if errors.As(setupErr, &protocolErr) {
		return tailcatErr
	}
	if !c.tailcatFallbackAllowed() {
		return tailcatErr
	}
	log.Debugf("Tailcat setup failed; falling back to croc relay: %v", tailcatErr)
	attempt.closeTailcatPending()
	attempt.cancelTailcatSetup()
	if err := message.Send(c.connection(0), c.Key, message.Message{
		Type:    message.TypeTransportSelect,
		Message: string(TransportRelay),
	}); err != nil {
		return c.tailcatError("relay fallback selection", err, tokenValue)
	}
	return c.activateRelayDataChannels(attempt)
}

func (c *Client) watchTailcatSelection(attempt *transferAttemptState) {
	if attempt == nil || attempt.tailcat.setupDone == nil {
		return
	}
	_, _, deadline := attempt.beginTailcatSetup(c.stop.ctx)
	go func() {
		fallbackAt := deadline
		if c.tailcatFallbackAllowed() {
			fallbackAt = deadline.Add(-time.Second)
		}
		timer := time.NewTimer(max(time.Until(fallbackAt), 0))
		defer timer.Stop()
		select {
		case <-attempt.tailcat.setupDone:
			return
		case <-timer.C:
			attempt.cancelTailcatSetup()
			if !c.tailcatFallbackAllowed() {
				attempt.report(c.tailcatError("transport selection", context.DeadlineExceeded, ""))
				return
			}
			log.Debug("Tailcat setup deadline approaching; requesting croc relay fallback")
			if err := attempt.sendTailcatStatus(c.connection(0), c.Key, tailcatStatusFallback); err != nil {
				attempt.report(c.tailcatError("relay fallback request", err, ""))
				return
			}
			timer.Reset(max(time.Until(deadline), 0))
			select {
			case <-attempt.tailcat.setupDone:
				return
			case <-timer.C:
				attempt.report(c.tailcatError("transport selection", context.DeadlineExceeded, ""))
			case <-c.stop.ctx.Done():
				return
			}
		case <-c.stop.ctx.Done():
			return
		}
	}()
}

func (c *Client) finishTailcatReceiverDial(bundle *tailcatDataBundle, cancel context.CancelFunc, tokenValue string, attempt *transferAttemptState) error {
	if err := attempt.setTailcatPending(bundle, cancel); err != nil {
		return c.tailcatError("peer connection", err, tokenValue)
	}
	if c.peerImplicitTailcatReady && c.peerStagedTransport && !c.peerRequiresTailcat() {
		return nil
	}
	if err := attempt.sendTailcatStatus(c.connection(0), c.Key, tailcatStatusReady); err != nil {
		attempt.closeTailcatPending()
		return c.tailcatError("peer readiness", err, tokenValue)
	}
	return nil
}

func (c *Client) finishTailcatReceiverFailure(setupErr error, tokenValue string, attempt *transferAttemptState) error {
	tailcatErr := c.tailcatError("peer connection", setupErr, tokenValue)
	if !c.tailcatFallbackAllowed() {
		return tailcatErr
	}
	attempt.cancelTailcatSetup()
	log.Debugf("Tailcat setup failed; requesting croc relay fallback: %v", tailcatErr)
	if err := attempt.sendTailcatStatus(c.connection(0), c.Key, tailcatStatusFallback); err != nil {
		return c.tailcatError("relay fallback request", err, tokenValue)
	}
	return nil
}

func (c *Client) processTailcatOffer(m message.Message, attempt *transferAttemptState) (err error) {
	defer func() {
		if err != nil {
			attempt.cancelTailcatSetup()
		}
	}()
	if c.Options.IsSender {
		return errors.New("sender rejected a Tailcat offer from the receiver")
	}
	if !c.localTailcatSupported() || !c.tailcat.peerCapable {
		return errors.New("Tailcat offer received without negotiated Tailcat mode")
	}
	if c.tailcat.offerReceived {
		return errors.New("duplicate Tailcat offer rejected")
	}
	c.tailcat.offerReceived = true
	tokenValue := m.Message
	if err := c.validateTailcatOffer(tokenValue); err != nil {
		return c.tailcatError("offer validation", err, tokenValue)
	}

	setupCtx, cancel, deadline := attempt.beginTailcatSetup(c.stop.ctx)
	dialResult := make(chan tailcatDialResult)
	go func() {
		bundle, err := c.dialTailcatData(setupCtx, tokenValue)
		result := tailcatDialResult{bundle: bundle, err: err}
		select {
		case dialResult <- result:
		case <-setupCtx.Done():
			if bundle != nil {
				_ = bundle.Close()
			}
		}
	}()
	if c.peerImplicitTailcatReady && c.peerStagedTransport && !c.peerRequiresTailcat() {
		go func() {
			timer := time.NewTimer(time.Until(deadline))
			defer timer.Stop()
			select {
			case result := <-dialResult:
				if result.err != nil {
					if finishErr := c.finishTailcatReceiverFailure(result.err, tokenValue, attempt); finishErr != nil {
						attempt.report(finishErr)
					}
					return
				}
				if finishErr := c.finishTailcatReceiverDial(result.bundle, cancel, tokenValue, attempt); finishErr != nil {
					attempt.report(finishErr)
				}
			case <-timer.C:
				cancel()
				if finishErr := c.finishTailcatReceiverFailure(context.DeadlineExceeded, tokenValue, attempt); finishErr != nil {
					attempt.report(finishErr)
				}
			case <-c.stop.ctx.Done():
				cancel()
			}
		}()
		return nil
	}
	timer := time.NewTimer(time.Until(deadline))
	select {
	case result := <-dialResult:
		timer.Stop()
		if result.err != nil {
			cancel()
			return c.finishTailcatReceiverFailure(result.err, tokenValue, attempt)
		}
		return c.finishTailcatReceiverDial(result.bundle, cancel, tokenValue, attempt)
	case <-timer.C:
		cancel()
		return c.finishTailcatReceiverFailure(context.DeadlineExceeded, tokenValue, attempt)
	case <-c.stop.ctx.Done():
		timer.Stop()
		cancel()
		return c.tailcatError("peer connection", c.stop.ctx.Err(), tokenValue)
	}
}

func (c *Client) processTransportSelect(m message.Message, attempt *transferAttemptState) error {
	if c.Options.IsSender {
		return errors.New("sender rejected a transport selection from the receiver")
	}
	if !c.tailcat.peerCapable {
		return errors.New("transport selection received without negotiated Tailcat support")
	}
	if c.transportSelectionReceived {
		return errors.New("duplicate transport selection rejected")
	}
	c.transportSelectionReceived = true

	switch TransportMode(m.Message) {
	case TransportDERP:
		bundle, cancel := attempt.takeTailcatPending()
		if bundle == nil && c.peerImplicitTailcatReady {
			select {
			case <-attempt.tailcatPendingReady():
				bundle, cancel = attempt.takeTailcatPending()
			case <-time.After(stagedSelectionTimeout):
				return errors.New("Tailcat selected before receiver connection became ready")
			case <-c.stop.ctx.Done():
				return c.stop.ctx.Err()
			}
		}
		if bundle == nil {
			if cancel != nil {
				cancel()
			}
			return errors.New("Tailcat selected without a ready connection")
		}
		log.Debug("selected Tailcat data transport")
		if err := c.installTailcatBundle(bundle, cancel, attempt); err != nil {
			return err
		}
		return c.finishDataTransportActivation()
	case TransportRelay:
		if !c.tailcatFallbackAllowed() {
			return errors.New("peer selected relay when Tailcat fallback is not allowed")
		}
		log.Debug("selected croc relay data transport after Tailcat fallback")
		if c.peerStagedTransport {
			return c.commitStagedRelayReceiver(attempt)
		}
		attempt.closeTailcatPending()
		attempt.cancelTailcatSetup()
		attempt.finishTailcatSetup()
		return c.activateRelayDataChannels(attempt)
	default:
		return fmt.Errorf("invalid transport selection %q", m.Message)
	}
}

func (c *Client) processUnexpectedTailcatStatus(m message.Message) error {
	if !c.Options.IsSender {
		return errors.New("receiver rejected a Tailcat status from the sender")
	}
	if c.selectedDataTransport.Load() == selectedTransportRelay &&
		(m.Message == tailcatStatusReady || m.Message == tailcatStatusFallback) {
		log.Debugf("ignoring late Tailcat status after relay selection: %s", m.Message)
		return nil
	}
	return fmt.Errorf("unexpected Tailcat status %q", m.Message)
}
