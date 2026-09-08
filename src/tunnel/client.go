package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"golang.org/x/crypto/ssh"
)

// Session is one authenticated guest. Connections are never replayed after loss.
type Session struct {
	conn         ssh.Conn
	Port         int
	ended        atomic.Bool
	requestsDone chan struct{}
}

// Authenticate joins over an already-connected relay byte stream. It is shared
// by native clients and WASM, so browser and CLI authentication cannot drift.
func Authenticate(ctx context.Context, connection net.Conn, code, curve string, dialRoom func(context.Context, string) (net.Conn, error)) (*Session, error) {
	components, err := codephrase.ParseTunnel(code)
	if err != nil {
		return nil, err
	}
	if curve == "" {
		curve = "p256"
	}
	control := connection
	stop := context.AfterFunc(ctx, func() { _ = control.Close() })
	defer stop()
	success := false
	defer func() {
		if !success && connection != nil {
			_ = connection.Close()
		}
	}()
	c := comm.New(connection)
	key, deadline, err := guestPAKE(c, components, curve)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	credential := make([]byte, 32)
	if _, err = rand.Read(credential); err != nil {
		return nil, err
	}
	defer clear(credential)
	if err = sendMessageUntil(c, key, message.Message{Type: "tunnel-authorize", Version: protocolVersion, Bytes: credential}, deadline); err != nil {
		return nil, err
	}
	offer, err := receiveMessageUntil(c, key, deadline)
	if err != nil {
		return nil, err
	}
	if offer.Type == message.TypeError {
		return nil, errors.New(offer.Message)
	}
	if offer.Type != "tunnel-offer" || offer.Version != protocolVersion || offer.Num < 1 || offer.Num > 65535 || len(offer.Bytes) > 16384 {
		return nil, errors.New("invalid tunnel offer")
	}
	hostKey, err := ssh.ParsePublicKey(offer.Bytes)
	if err != nil {
		return nil, errors.New("invalid tunnel host key")
	}
	roomBytes, err := hex.DecodeString(offer.Message)
	if err != nil || len(roomBytes) != 32 || dialRoom == nil {
		return nil, errors.New("invalid tunnel data room")
	}
	// Release the invitation room before opening this guest's private stream.
	_ = connection.Close()
	connection, err = dialRoom(ctx, offer.Message)
	if err != nil {
		return nil, err
	}
	if err := prepareStream(connection); err != nil {
		return nil, err
	}
	s, err := connectSSH(ctx, connection, hostKey, credential, offer.Num)
	if err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func connectSSH(ctx context.Context, connection net.Conn, hostKey ssh.PublicKey, credential []byte, port int) (*Session, error) {
	if len(credential) != 32 || hostKey == nil {
		return nil, errors.New("invalid tunnel authentication")
	}
	if err := connection.SetDeadline(time.Now().Add(authTimeout)); err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{User: "croc-tunnel", Auth: []ssh.AuthMethod{ssh.Password(base64.RawStdEncoding.EncodeToString(credential))}, HostKeyAlgorithms: []string{hostKey.Type()}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if !bytes.Equal(key.Marshal(), hostKey.Marshal()) {
			return errors.New("tunnel host key does not match authenticated invitation")
		}
		return nil
	}}
	conn, channels, requests, err := ssh.NewClientConn(connection, "croc-tunnel", config)
	if err != nil {
		return nil, err
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	s := &Session{conn: conn, Port: port, requestsDone: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	go func() { _ = conn.Wait(); stop() }()
	go func() {
		for ch := range channels {
			_ = ch.Reject(ssh.Prohibited, "guest channels are not supported")
		}
	}()
	go func() {
		defer close(s.requestsDone)
		for req := range requests {
			if req.Type == endedRequest {
				s.ended.Store(true)
			}
			if req.WantReply {
				_ = req.Reply(req.Type == endedRequest, nil)
			}
		}
	}()
	return s, nil
}

func (s *Session) Close() error { return s.conn.Close() }
func (s *Session) Wait() error {
	err := s.conn.Wait()
	<-s.requestsDone
	if s.ended.Load() {
		return nil
	}
	if err == nil {
		return errors.New("tunnel connection lost")
	}
	return err
}

// Open opens only a named service channel, never a caller-chosen destination.
func (s *Session) Open(ctx context.Context, kind string, metadata []byte) (ssh.Channel, error) {
	if len(metadata) > maxControl {
		return nil, errors.New("tunnel metadata is too large")
	}
	if kind != channelTCP && kind != ChannelHTTP && kind != ChannelWebSocket {
		return nil, errors.New("unsupported tunnel channel")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// SSH OpenChannel has no context API. A bounded cancellation closes the
	// transport as well, rather than leaving an abandoned open request behind.
	opening, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()
	stop := context.AfterFunc(opening, func() { _ = s.Close() })
	ch, requests, err := s.conn.OpenChannel(kind, metadata)
	if !stop() && err == nil {
		_ = ch.Close()
		return nil, opening.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("open tunnel channel: %w", err)
	}
	go ssh.DiscardRequests(requests)
	return ch, nil
}
func (s *Session) OpenTCP(ctx context.Context) (ssh.Channel, error) {
	return s.Open(ctx, channelTCP, nil)
}
