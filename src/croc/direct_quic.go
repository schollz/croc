package croc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/logger"

	"github.com/schollz/croc/v11/src/directquic"
	"github.com/schollz/croc/v11/src/message"
)

const directQUICSetupTimeout = 4 * time.Second

func (c *Client) hasDirectQUICPayload() bool {
	for _, file := range c.FilesToTransfer {
		if file.Size > 0 && file.Symlink == "" {
			return true
		}
	}
	return false
}

func (c *Client) directQUICFeatures() []string {
	features := []string{perFileCompressionFeature}
	c.directMu.Lock()
	enabled := c.directSession != nil || c.directSelected
	c.directMu.Unlock()
	if enabled {
		features = append(features, directQUICFeature)
	}
	return features
}

func (c *Client) currentDirectQUICOffer() *DirectQUICOffer {
	c.directMu.Lock()
	defer c.directMu.Unlock()
	if c.directSession == nil || c.directSelected {
		return nil
	}
	offer := c.directSession.Offer()
	return &offer
}

func (c *Client) prepareDirectQUIC(role directquic.Role, sessionID []byte) (*DirectQUICOffer, error) {
	if !c.Options.ExperimentalDirectUDP || !c.hasDirectQUICPayload() {
		return nil, nil
	}
	if !directquic.Supported() {
		return nil, directquic.ErrUnsupported
	}
	disableSTUN := c.Options.OnlyLocal || strings.EqualFold(strings.TrimSpace(c.Options.ExperimentalSTUNServer), "off")
	started := time.Now()
	session, err := directquic.New(c.stop.ctx, directquic.Config{
		Role:        role,
		Key:         c.Key,
		SessionID:   sessionID,
		STUNServer:  c.Options.ExperimentalSTUNServer,
		DisableSTUN: disableSTUN,
	})
	if err != nil {
		return nil, err
	}
	c.directMu.Lock()
	if c.directSession != nil {
		c.directMu.Unlock()
		_ = session.Close()
		return nil, errors.New("direct QUIC session already exists")
	}
	c.directSession = session
	c.directMu.Unlock()
	offer := session.Offer()
	log.Debugf("direct QUIC gathered %s in %s", directQUICCandidateSummary(offer.Candidates), time.Since(started))
	return &offer, nil
}

func (c *Client) prepareDirectQUICSender() *DirectQUICOffer {
	offer, err := c.prepareDirectQUIC(directquic.RoleSender, nil)
	if err == nil {
		return offer
	}
	c.directQUICStatus("Experimental direct UDP unavailable (%v); using TCP.", err)
	log.Debugf("direct QUIC sender preflight failed: %v", err)
	return nil
}

func (c *Client) prepareDirectQUICReceiver(peer *DirectQUICOffer, attempt *transferAttemptState) error {
	if !c.Options.ExperimentalDirectUDP || !c.hasDirectQUICPayload() {
		return nil
	}
	if peer == nil {
		c.directQUICStatus("Peer did not enable experimental direct UDP; using TCP.")
		return nil
	}
	if !directquic.Supported() {
		c.directQUICStatus("Experimental direct UDP is unsupported on this platform; using TCP.")
		return nil
	}
	_, err := c.prepareDirectQUIC(directquic.RoleReceiver, peer.SessionID)
	if err != nil {
		if errors.Is(err, directquic.ErrUnsupported) || errors.Is(err, directquic.ErrNoUDPSockets) {
			c.directQUICStatus("Experimental direct UDP unavailable (%v); using TCP.", err)
			return nil
		}
		return fmt.Errorf("experimental direct UDP setup failed: %w", err)
	}

	c.directMu.Lock()
	session := c.directSession
	acceptResult := make(chan directQUICConnectResult, 1)
	c.directAccept = acceptResult
	c.directMu.Unlock()
	peerCopy := cloneDirectQUICOffer(peer)
	go func() {
		conn, acceptErr := session.Accept(c.stop.ctx, *peerCopy)
		acceptResult <- directQUICConnectResult{conn: conn, err: acceptErr}
		if acceptErr != nil && c.ctxErr() == nil {
			attempt.report(fmt.Errorf("experimental direct UDP setup failed: %w", acceptErr))
		}
	}()
	return nil
}

func (c *Client) selectDirectQUIC(peer *DirectQUICOffer) error {
	c.directMu.Lock()
	if c.directSelected {
		c.directMu.Unlock()
		return nil
	}
	session := c.directSession
	c.directMu.Unlock()
	if session == nil {
		return nil
	}
	if peer == nil {
		c.directQUICStatus("Peer did not enable experimental direct UDP; using TCP.")
		c.closeDirectQUIC()
		return nil
	}

	setupCtx, cancel := context.WithTimeout(c.stop.ctx, directQUICSetupTimeout)
	defer cancel()
	started := time.Now()
	conn, err := session.Dial(setupCtx, *cloneDirectQUICOffer(peer))
	if err != nil {
		log.Debugf("direct QUIC setup failed after %s: %v", time.Since(started), err)
		return fmt.Errorf("experimental direct UDP setup failed: %w", err)
	}
	log.Debugf("direct QUIC setup completed in %s", time.Since(started))
	lanes := len(c.Options.RelayPorts)
	if lanes < 1 || c.Options.NoMultiplexing {
		lanes = 1
	}
	if lanes > directquic.MaxLanes {
		lanes = directquic.MaxLanes
	}

	c.directMu.Lock()
	c.directConn = conn
	c.directSelected = true
	c.directLaneCount = lanes
	c.usedDirectQUIC.Store(true)
	c.directMu.Unlock()
	selection := DirectQUICSelection{
		Transport: directQUICFeature,
		SessionID: session.Offer().SessionID,
		LaneCount: lanes,
	}
	payload, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	if err = message.Send(c.conn[0], c.Key, message.Message{Type: message.TypeTransportSelected, Bytes: payload}); err != nil {
		return err
	}
	c.directQUICStatus("Using experimental direct UDP (QUIC) to %s.", conn.RemoteAddr())
	return nil
}

func (c *Client) processDirectQUICSelection(payload []byte, attempt *transferAttemptState) error {
	if c.Options.IsSender {
		return errors.New("sender received an unexpected transport selection")
	}
	var selection DirectQUICSelection
	if err := json.Unmarshal(payload, &selection); err != nil {
		return err
	}
	if selection.Transport != directQUICFeature || selection.LaneCount < 1 || selection.LaneCount > directquic.MaxLanes {
		return errors.New("peer selected an invalid experimental transport")
	}
	c.directMu.Lock()
	alreadySelected := c.directSelected
	session := c.directSession
	acceptResult := c.directAccept
	c.directMu.Unlock()
	if alreadySelected {
		return errors.New("peer sent a duplicate experimental transport selection")
	}
	if session == nil || acceptResult == nil || !bytes.Equal(selection.SessionID, session.Offer().SessionID) {
		return errors.New("peer selected an unnegotiated experimental transport")
	}

	waitCtx, cancel := context.WithTimeout(c.stop.ctx, directQUICSetupTimeout)
	defer cancel()
	var result directQUICConnectResult
	select {
	case result = <-acceptResult:
	case <-waitCtx.Done():
		return fmt.Errorf("experimental direct UDP accept timed out: %w", waitCtx.Err())
	}
	if result.err != nil {
		return fmt.Errorf("experimental direct UDP setup failed: %w", result.err)
	}
	c.directMu.Lock()
	c.directConn = result.conn
	c.directSelected = true
	c.directLaneCount = selection.LaneCount
	c.usedDirectQUIC.Store(true)
	c.directMu.Unlock()
	c.directQUICStatus("Using experimental direct UDP (QUIC) from %s.", result.conn.RemoteAddr())
	c.startDirectQUICReceive(c.FilesToTransferCurrentNum, selection.LaneCount, attempt)
	return nil
}

func (c *Client) startDirectQUICReceive(fileIndex, laneCount int, attempt *transferAttemptState) {
	c.directMu.Lock()
	if !c.directSelected || c.directConn == nil || c.directSession == nil || c.directReceivingFile == fileIndex {
		c.directMu.Unlock()
		return
	}
	c.directReceivingFile = fileIndex
	conn := c.directConn
	sessionID := c.directSession.Offer().SessionID
	c.directMu.Unlock()

	go func() {
		var wg sync.WaitGroup
		lanes := make(map[uint16]struct{}, laneCount)
		for i := 0; i < laneCount; i++ {
			stream, header, err := conn.AcceptStream(c.stop.ctx)
			if err != nil {
				if c.ctxErr() == nil {
					attempt.report(transferDisconnectError{err: fmt.Errorf("accept direct QUIC stream: %w", err)})
				}
				return
			}
			if !bytes.Equal(header.SessionID, sessionID) || int(header.FileIndex) != fileIndex || int(header.LaneCount) != laneCount {
				_ = stream.Close()
				attempt.report(fmt.Errorf("invalid direct QUIC stream header for file %d", fileIndex))
				return
			}
			if _, exists := lanes[header.Lane]; exists {
				_ = stream.Close()
				attempt.report(fmt.Errorf("duplicate direct QUIC lane %d", header.Lane))
				return
			}
			lanes[header.Lane] = struct{}{}
			wg.Add(1)
			go func(lane int, r io.ReadCloser) {
				defer wg.Done()
				defer r.Close()
				c.receiveDirectQUICStream(lane, r, attempt)
			}(int(header.Lane), stream)
		}
		wg.Wait()
		c.receiveMutex.Lock()
		incomplete := c.FilesToTransferCurrentNum == fileIndex && !c.CurrentFileIsClosed
		c.receiveMutex.Unlock()
		if incomplete && c.ctxErr() == nil {
			attempt.report(transferDisconnectError{err: fmt.Errorf("direct QUIC streams ended before file %d completed", fileIndex)})
		}
	}()
}

func (c *Client) receiveDirectQUICStream(lane int, stream io.Reader, attempt *transferAttemptState) {
	var receiveBuffer []byte
	var decompressedBuffer []byte
	for {
		data, err := directquic.ReadFrame(stream, receiveBuffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			if c.ctxErr() == nil {
				attempt.report(transferDisconnectError{err: fmt.Errorf("read direct QUIC frame: %w", err)})
			}
			return
		}
		receiveBuffer = data
		var stop bool
		decompressedBuffer, stop = c.processReceivedDataChunk(lane, data, decompressedBuffer, attempt)
		if stop {
			return
		}
	}
}

func (c *Client) sendDirectQUICData(fread *os.File, attempt *transferAttemptState) {
	c.directMu.Lock()
	conn := c.directConn
	session := c.directSession
	lanes := c.directLaneCount
	c.directMu.Unlock()
	if conn == nil || session == nil || lanes < 1 {
		attempt.report(errors.New("direct QUIC transport was selected without a connection"))
		return
	}
	sessionID := session.Offer().SessionID
	fileIndex := c.FilesToTransferCurrentNum
	fileTransfer := newSenderFileTransfer(fread, lanes)
	streams := make([]io.WriteCloser, lanes)
	for lane := 0; lane < lanes; lane++ {
		stream, err := conn.OpenStream(c.stop.ctx, directquic.StreamHeader{
			SessionID: sessionID,
			FileIndex: uint32(fileIndex),
			Lane:      uint16(lane),
			LaneCount: uint16(lanes),
		})
		if err != nil {
			for _, opened := range streams {
				if opened != nil {
					_ = opened.Close()
				}
			}
			for range lanes {
				fileTransfer.done()
			}
			attempt.report(transferDisconnectError{err: fmt.Errorf("open direct QUIC stream: %w", err)})
			return
		}
		streams[lane] = stream
	}
	for lane, stream := range streams {
		go func(lane int, stream io.WriteCloser) {
			c.sendDataLane(lane, lanes, fread, attempt, fileTransfer, func(data []byte) error {
				return directquic.WriteFrame(stream, data)
			})
			if closeErr := stream.Close(); closeErr != nil && c.ctxErr() == nil {
				attempt.report(transferDisconnectError{err: fmt.Errorf("close direct QUIC stream: %w", closeErr)})
			}
		}(lane, stream)
	}
}

func (c *Client) directQUICSelected() bool {
	c.directMu.Lock()
	defer c.directMu.Unlock()
	return c.directSelected
}

func (c *Client) closeDirectQUIC() {
	c.directMu.Lock()
	conn := c.directConn
	session := c.directSession
	c.directConn = nil
	c.directSession = nil
	c.directAccept = nil
	c.directSelected = false
	c.directLaneCount = 0
	c.directReceivingFile = -1
	c.directMu.Unlock()
	if conn != nil {
		stats := conn.Stats()
		log.Debugf("direct QUIC stats: rtt=%s sent_bytes=%d recv_bytes=%d lost_bytes=%d sent_packets=%d lost_packets=%d gso=%t",
			stats.SmoothedRTT, stats.BytesSent, stats.BytesReceived, stats.BytesLost, stats.PacketsSent, stats.PacketsLost, stats.GSO)
	}
	if session != nil {
		_ = session.Close()
	} else if conn != nil {
		_ = conn.Close()
	}
}

func (c *Client) directQUICStatus(format string, args ...any) {
	if c.Options.Quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "\n"+format+"\n", args...)
}

func cloneDirectQUICOffer(offer *DirectQUICOffer) *DirectQUICOffer {
	if offer == nil {
		return nil
	}
	clone := &DirectQUICOffer{
		SessionID:  append([]byte(nil), offer.SessionID...),
		CertSHA256: append([]byte(nil), offer.CertSHA256...),
		Candidates: append([]directquic.Candidate(nil), offer.Candidates...),
	}
	return clone
}

func directQUICCandidateSummary(candidates []directquic.Candidate) string {
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.Network+"/"+candidate.Kind]++
	}
	parts := make([]string, 0, 4)
	for _, key := range []string{"udp4/host", "udp4/srflx", "udp6/host", "udp6/srflx"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
		}
	}
	if len(parts) == 0 {
		return "0 candidates"
	}
	return strings.Join(parts, " ")
}
