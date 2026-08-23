// Package directquic implements croc's experimental authenticated direct QUIC
// data path. Signaling remains on croc's PAKE-protected control connection.
package directquic

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"runtime"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"
	"golang.org/x/crypto/hkdf"
)

const (
	Feature           = "direct-quic-v1"
	DefaultSTUNServer = "stun.cloudflare.com:3478"
	MaxCandidates     = 16
	MaxLanes          = 8

	probeTimeout          = 2 * time.Second
	defaultGatherTimeout  = time.Second
	defaultKeepAlive      = 15 * time.Second
	probeInterval         = 50 * time.Millisecond
	udpSocketBuffer       = 7 * 1024 * 1024
	applicationProtocol   = "croc-direct-quic-v1"
	applicationErrorClose = quic.ApplicationErrorCode(0)
)

var (
	ErrUnsupported  = errors.New("direct QUIC is unsupported on this platform")
	ErrNoUDPSockets = errors.New("direct QUIC has no usable UDP sockets")
	// The first byte deliberately leaves QUIC's fixed bit (0x40) clear so
	// quic-go delivers probes through ReadNonQUICPacket.
	probeMagic = [8]byte{'!', 'c', 'r', 'o', 'c', 'd', 'u', '1'}
)

type Role byte

const (
	RoleSender Role = iota
	RoleReceiver
)

type Candidate struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

type Offer struct {
	SessionID  []byte      `json:"session_id"`
	CertSHA256 []byte      `json:"cert_sha256"`
	Candidates []Candidate `json:"candidates"`
}

type Config struct {
	Role          Role
	Key           []byte
	SessionID     []byte
	STUNServer    string
	DisableSTUN   bool
	GatherTimeout time.Duration
}

type endpoint struct {
	network   string
	conn      *net.UDPConn
	transport *quic.Transport
	stunAddr  *net.UDPAddr
}

type Session struct {
	ctx         context.Context
	cancel      context.CancelFunc
	role        Role
	sessionID   [32]byte
	probeKey    [32]byte
	certificate tls.Certificate
	fingerprint [sha256.Size]byte

	mu         sync.Mutex
	endpoints  []*endpoint
	candidates []Candidate
	listeners  []*quic.Listener
	conn       *Conn
	closed     bool
}

type Conn struct {
	conn *quic.Conn
	once sync.Once
}

type Stats struct {
	MinRTT, LatestRTT, SmoothedRTT, MeanDeviation time.Duration
	BytesSent, PacketsSent                        uint64
	BytesReceived, PacketsReceived                uint64
	BytesLost, PacketsLost                        uint64
	GSO                                           bool
}

type connectResult struct {
	conn *quic.Conn
	err  error
}

type validatedRoute struct {
	endpoint *endpoint
	addr     *net.UDPAddr
}

func Supported() bool {
	return runtime.GOOS != "js" && runtime.GOOS != "plan9"
}

func New(ctx context.Context, cfg Config) (*Session, error) {
	if !Supported() {
		return nil, ErrUnsupported
	}
	if cfg.Role != RoleSender && cfg.Role != RoleReceiver {
		return nil, fmt.Errorf("invalid direct QUIC role %d", cfg.Role)
	}
	if len(cfg.Key) == 0 {
		return nil, errors.New("direct QUIC requires a PAKE key")
	}
	if !cfg.DisableSTUN {
		server := cfg.STUNServer
		if server == "" {
			server = DefaultSTUNServer
		}
		if err := ValidateSTUNServer(server); err != nil {
			return nil, err
		}
	}

	s := &Session{role: cfg.Role}
	s.ctx, s.cancel = context.WithCancel(ctx)
	if len(cfg.SessionID) == 0 {
		if cfg.Role != RoleSender {
			s.cancel()
			return nil, errors.New("direct QUIC receiver requires a session ID")
		}
		if _, err := rand.Read(s.sessionID[:]); err != nil {
			s.cancel()
			return nil, fmt.Errorf("generate direct QUIC session ID: %w", err)
		}
	} else if len(cfg.SessionID) != len(s.sessionID) {
		s.cancel()
		return nil, fmt.Errorf("invalid direct QUIC session ID length %d", len(cfg.SessionID))
	} else {
		copy(s.sessionID[:], cfg.SessionID)
	}

	if _, err := io.ReadFull(hkdf.New(sha256.New, cfg.Key, s.sessionID[:], []byte("croc/direct-quic/probe/v1")), s.probeKey[:]); err != nil {
		s.cancel()
		return nil, fmt.Errorf("derive direct QUIC probe key: %w", err)
	}

	certificate, fingerprint, err := newCertificate()
	if err != nil {
		s.cancel()
		return nil, err
	}
	s.certificate = certificate
	s.fingerprint = fingerprint

	for _, spec := range []struct {
		network string
		addr    *net.UDPAddr
	}{
		{network: "udp4", addr: &net.UDPAddr{IP: net.IPv4zero}},
		{network: "udp6", addr: &net.UDPAddr{IP: net.IPv6zero}},
	} {
		udpConn, listenErr := net.ListenUDP(spec.network, spec.addr)
		if listenErr != nil {
			continue
		}
		_ = udpConn.SetReadBuffer(udpSocketBuffer)
		_ = udpConn.SetWriteBuffer(udpSocketBuffer)
		s.endpoints = append(s.endpoints, &endpoint{
			network: spec.network,
			conn:    udpConn,
			transport: &quic.Transport{
				Conn: udpConn,
			},
		})
	}
	if len(s.endpoints) == 0 {
		s.cancel()
		return nil, fmt.Errorf("%w: could not open UDP4 or UDP6", ErrNoUDPSockets)
	}

	s.gatherHostCandidates()
	if !cfg.DisableSTUN {
		server := cfg.STUNServer
		if server == "" {
			server = DefaultSTUNServer
		}
		gatherTimeout := cfg.GatherTimeout
		if gatherTimeout <= 0 {
			gatherTimeout = defaultGatherTimeout
		}
		gatherCtx, cancel := context.WithTimeout(s.ctx, gatherTimeout)
		s.gatherServerReflexiveCandidates(gatherCtx, server)
		cancel()
	}
	if len(s.candidates) == 0 {
		s.Close()
		return nil, fmt.Errorf("%w: no usable candidates", ErrNoUDPSockets)
	}
	go s.keepMappingsAlive()
	return s, nil
}

func (s *Session) Offer() Offer {
	s.mu.Lock()
	defer s.mu.Unlock()
	offer := Offer{
		SessionID:  append([]byte(nil), s.sessionID[:]...),
		CertSHA256: append([]byte(nil), s.fingerprint[:]...),
		Candidates: make([]Candidate, len(s.candidates)),
	}
	copy(offer.Candidates, s.candidates)
	return offer
}

func (s *Session) Dial(ctx context.Context, peer Offer) (*Conn, error) {
	if s.role != RoleSender {
		return nil, errors.New("only the direct QUIC sender can dial")
	}
	peer, err := validateOffer(peer, s.sessionID)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	routes := s.startProbing(probeCtx, peer.Candidates)
	var route validatedRoute
	select {
	case route = <-routes:
	case <-probeCtx.Done():
		return nil, fmt.Errorf("direct UDP probe timed out: %w", probeCtx.Err())
	}

	conn, err := route.endpoint.transport.Dial(ctx, route.addr, s.clientTLSConfig(peer.CertSHA256), quicConfig())
	if err != nil {
		return nil, fmt.Errorf("direct QUIC handshake: %w", err)
	}
	wrapped := &Conn{conn: conn}
	if !s.setConn(wrapped) {
		_ = wrapped.Close()
		return nil, context.Canceled
	}
	return wrapped, nil
}

func (s *Session) Accept(ctx context.Context, peer Offer) (*Conn, error) {
	if s.role != RoleReceiver {
		return nil, errors.New("only the direct QUIC receiver can accept")
	}
	peer, err := validateOffer(peer, s.sessionID)
	if err != nil {
		return nil, err
	}

	endpoints := s.endpointSnapshot()
	accepts := make(chan connectResult, len(endpoints))
	var createdListeners []*quic.Listener
	for _, ep := range endpoints {
		listener, listenErr := ep.transport.Listen(s.serverTLSConfig(peer.CertSHA256), quicConfig())
		if listenErr != nil {
			continue
		}
		s.mu.Lock()
		s.listeners = append(s.listeners, listener)
		s.mu.Unlock()
		createdListeners = append(createdListeners, listener)
		go func(l *quic.Listener) {
			conn, acceptErr := l.Accept(ctx)
			accepts <- connectResult{conn: conn, err: acceptErr}
		}(listener)
	}
	if len(createdListeners) == 0 {
		return nil, errors.New("direct QUIC could not open a listener")
	}
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.startProbing(probeCtx, peer.Candidates)

	remaining := len(createdListeners)
	for remaining > 0 {
		select {
		case result := <-accepts:
			remaining--
			if result.err != nil {
				continue
			}
			wrapped := &Conn{conn: result.conn}
			if !s.setConn(wrapped) {
				_ = wrapped.Close()
				return nil, context.Canceled
			}
			s.closeOtherListeners()
			return wrapped, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("direct QUIC listeners stopped before accepting a connection")
}

func (s *Session) setConn(conn *Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conn = conn
	return true
}

func (s *Session) closeOtherListeners() {
	s.mu.Lock()
	listeners := append([]*quic.Listener(nil), s.listeners...)
	s.listeners = nil
	s.mu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	conn := s.conn
	listeners := append([]*quic.Listener(nil), s.listeners...)
	endpoints := append([]*endpoint(nil), s.endpoints...)
	s.listeners = nil
	s.endpoints = nil
	s.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	for _, listener := range listeners {
		_ = listener.Close()
	}
	var firstErr error
	for _, ep := range endpoints {
		if err := ep.transport.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := ep.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *Conn) OpenStream(ctx context.Context, header StreamHeader) (io.WriteCloser, error) {
	stream, err := c.conn.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	if err = WriteStreamHeader(stream, header); err != nil {
		stream.CancelWrite(1)
		return nil, err
	}
	return stream, nil
}

type ReceiveStream struct {
	stream *quic.ReceiveStream
}

func (s *ReceiveStream) Read(p []byte) (int, error) { return s.stream.Read(p) }

func (s *ReceiveStream) Close() error {
	s.stream.CancelRead(0)
	return nil
}

func (c *Conn) AcceptStream(ctx context.Context) (io.ReadCloser, StreamHeader, error) {
	stream, err := c.conn.AcceptUniStream(ctx)
	if err != nil {
		return nil, StreamHeader{}, err
	}
	header, err := ReadStreamHeader(stream)
	if err != nil {
		stream.CancelRead(1)
		return nil, StreamHeader{}, err
	}
	return &ReceiveStream{stream: stream}, header, nil
}

func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.conn.CloseWithError(applicationErrorClose, "")
	})
	return err
}

func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (c *Conn) Stats() Stats {
	stats := c.conn.ConnectionStats()
	return Stats{
		MinRTT: stats.MinRTT, LatestRTT: stats.LatestRTT,
		SmoothedRTT: stats.SmoothedRTT, MeanDeviation: stats.MeanDeviation,
		BytesSent: stats.BytesSent, PacketsSent: stats.PacketsSent,
		BytesReceived: stats.BytesReceived, PacketsReceived: stats.PacketsReceived,
		BytesLost: stats.BytesLost, PacketsLost: stats.PacketsLost,
		GSO: c.conn.ConnectionState().GSO,
	}
}

func quicConfig() *quic.Config {
	return &quic.Config{
		HandshakeIdleTimeout:           2 * time.Second,
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                10 * time.Second,
		InitialStreamReceiveWindow:     2 * 1024 * 1024,
		MaxStreamReceiveWindow:         16 * 1024 * 1024,
		InitialConnectionReceiveWindow: 8 * 1024 * 1024,
		MaxConnectionReceiveWindow:     64 * 1024 * 1024,
		MaxIncomingStreams:             -1,
		MaxIncomingUniStreams:          MaxLanes,
		InitialPacketSize:              1200,
		Allow0RTT:                      false,
		EnableDatagrams:                false,
	}
}

func newCertificate() (tls.Certificate, [sha256.Size]byte, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, [sha256.Size]byte{}, fmt.Errorf("generate direct QUIC certificate key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, [sha256.Size]byte{}, fmt.Errorf("generate direct QUIC certificate serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "croc-direct-quic"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, [sha256.Size]byte{}, fmt.Errorf("create direct QUIC certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}, sha256.Sum256(der), nil
}

func (s *Session) clientTLSConfig(fingerprint []byte) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{s.certificate},
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{applicationProtocol},
		ServerName:         "croc-direct-quic",
		InsecureSkipVerify: true, // Verification is the PAKE-authenticated certificate pin below.
		VerifyConnection:   verifyFingerprint(fingerprint),
	}
}

func (s *Session) serverTLSConfig(fingerprint []byte) *tls.Config {
	return &tls.Config{
		Certificates:     []tls.Certificate{s.certificate},
		MinVersion:       tls.VersionTLS13,
		NextProtos:       []string{applicationProtocol},
		ClientAuth:       tls.RequireAnyClientCert,
		VerifyConnection: verifyFingerprint(fingerprint),
	}
}

func verifyFingerprint(expected []byte) func(tls.ConnectionState) error {
	want := append([]byte(nil), expected...)
	return func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) != 1 {
			return fmt.Errorf("expected one direct QUIC peer certificate, got %d", len(state.PeerCertificates))
		}
		fingerprint := sha256.Sum256(state.PeerCertificates[0].Raw)
		if !bytes.Equal(fingerprint[:], want) {
			return errors.New("direct QUIC peer certificate fingerprint mismatch")
		}
		return nil
	}
}
