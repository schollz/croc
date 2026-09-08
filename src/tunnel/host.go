//go:build !js

package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/tcp"
	"golang.org/x/crypto/ssh"
)

type HostConfig struct {
	Port                        int
	RelayAddress, RelayPassword string
	Logf                        func(string, ...any)
}
type Host struct {
	config              HostConfig
	code, relay, target string
	signer              ssh.Signer
	ctx                 context.Context
	cancel              context.CancelFunc
	stopParent          func() bool
	wg                  sync.WaitGroup
	mu                  sync.Mutex
	closed              bool
	sessions            map[*ssh.ServerConn]struct{}
	slots               chan struct{}
}

func StartHost(ctx context.Context, config HostConfig) (*Host, error) {
	if config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("port must be between 1 and 65535")
	}
	probe, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("localhost", strconv.Itoa(config.Port)))
	if err != nil {
		return nil, fmt.Errorf("local service is unavailable: %w", err)
	}
	address := probe.RemoteAddr().(*net.TCPAddr)
	_ = probe.Close()
	if !address.IP.IsLoopback() {
		return nil, errors.New("the shared service must be on loopback")
	}
	code, err := codephrase.GenerateTunnel()
	if err != nil {
		return nil, err
	}
	relay, err := relayForCode(config.RelayAddress, code)
	if err != nil {
		return nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	hostCtx, cancel := context.WithCancel(context.Background())
	h := &Host{config: config, code: code, relay: relay, target: address.String(), signer: signer, ctx: hostCtx, cancel: cancel, sessions: make(map[*ssh.ServerConn]struct{}), slots: make(chan struct{}, MaxGuests)}
	h.wg.Add(1)
	go h.rendezvous()
	stopParent := context.AfterFunc(ctx, func() { _ = h.Close() })
	h.mu.Lock()
	h.stopParent = stopParent
	closed := h.closed
	h.mu.Unlock()
	if closed {
		stopParent()
	}
	return h, nil
}
func (h *Host) Code() string { return h.code }
func (h *Host) Wait() error  { <-h.ctx.Done(); h.wg.Wait(); return nil }
func (h *Host) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	stopParent := h.stopParent
	sessions := make([]*ssh.ServerConn, 0, len(h.sessions))
	for s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	var notify sync.WaitGroup
	for _, s := range sessions {
		notify.Add(1)
		go func() {
			defer notify.Done()
			timer := time.AfterFunc(time.Second, func() { _ = s.Close() })
			_, _, _ = s.SendRequest(endedRequest, false, nil)
			_ = s.Close()
			timer.Stop()
		}()
	}
	notify.Wait()
	h.cancel()
	if stopParent != nil {
		stopParent()
	}
	h.wg.Wait()
	return nil
}
func (h *Host) rendezvous() {
	defer h.wg.Done()
	for h.ctx.Err() == nil {
		c, _, _, _, err := tcp.ConnectToTCPServerControlContext(h.ctx, h.relay, h.config.RelayPassword, mustRoom(h.code), 10*time.Second)
		if err == nil {
			stop := context.AfterFunc(h.ctx, c.Close)
			var credential []byte
			roomBytes := make([]byte, 32)
			_, err = rand.Read(roomBytes)
			room := hex.EncodeToString(roomBytes)
			if err == nil {
				credential, err = h.authorize(c, room)
			}
			expires := time.Now().Add(authTimeout)
			stop()
			c.Close()
			if err == nil {
				select {
				case h.slots <- struct{}{}:
					h.wg.Add(1)
					go func() {
						defer h.wg.Done()
						defer func() { <-h.slots }()
						defer clear(credential)
						ctx, cancel := context.WithTimeout(h.ctx, authTimeout)
						defer cancel()
						data, _, _, _, err := tcp.ConnectToTCPServerControlContext(ctx, h.relay, h.config.RelayPassword, room, 10*time.Second)
						if err != nil {
							return
						}
						defer data.Close()
						stop := context.AfterFunc(h.ctx, data.Close)
						defer stop()
						if prepareStream(data.Connection()) == nil {
							h.serve(data.Connection(), credential, expires)
						}
					}()
				default:
					err = errors.New("tunnel guest limit reached")
					stop()
					c.Close()
				}
			} else {
				stop()
				c.Close()
			}
		}
		delay := 250 * time.Millisecond
		if err != nil {
			delay = 5 * time.Second
			if h.config.Logf != nil && h.ctx.Err() == nil {
				h.config.Logf("Tunnel rendezvous: %v", err)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-h.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (h *Host) authorize(c *comm.Comm, room string) ([]byte, error) {
	components, _ := codephrase.ParseTunnel(h.code)
	key, deadline, err := hostPAKE(c, components)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	request, err := receiveMessageUntil(c, key, deadline)
	if err != nil {
		return nil, err
	}
	if request.Type != "tunnel-authorize" || request.Version != protocolVersion || len(request.Bytes) != 32 {
		return nil, errors.New("invalid tunnel authorization")
	}
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed || len(h.slots) >= MaxGuests {
		_ = sendMessageUntil(c, key, message.Message{Type: message.TypeError, Message: "Tunnel is unavailable or full"}, deadline)
		return nil, errors.New("tunnel unavailable")
	}
	err = sendMessageUntil(c, key, message.Message{Type: "tunnel-offer", Version: protocolVersion, Message: room, Bytes: h.signer.PublicKey().Marshal(), Num: h.config.Port}, deadline)
	return request.Bytes, err
}
func (h *Host) sshConfig(credential []byte, expires time.Time) *ssh.ServerConfig {
	used := false
	config := &ssh.ServerConfig{MaxAuthTries: 1, PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		decoded, err := base64.RawStdEncoding.DecodeString(string(password))
		defer clear(decoded)
		if used || time.Now().After(expires) || err != nil || subtle.ConstantTimeCompare(decoded, credential) != 1 {
			return nil, errors.New("invalid tunnel credential")
		}
		used = true
		return nil, nil
	}}
	config.AddHostKey(h.signer)
	return config
}
func (h *Host) serve(raw net.Conn, credential []byte, expires time.Time) {
	config := h.sshConfig(credential, expires)
	_ = raw.SetDeadline(expires)
	conn, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = raw.SetDeadline(time.Time{})
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.sessions[conn] = struct{}{}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.sessions, conn); h.mu.Unlock() }()
	go ssh.DiscardRequests(requests)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	go func() { _ = conn.Wait(); cancel() }()
	slots := make(chan struct{}, MaxChannels)
	var wg sync.WaitGroup
	defer wg.Wait()
	for incoming := range channels {
		kind := incoming.ChannelType()
		if len(incoming.ExtraData()) > maxControl || (kind != channelTCP && kind != ChannelHTTP && kind != ChannelWebSocket) || (kind == channelTCP && len(incoming.ExtraData()) != 0) {
			_ = incoming.Reject(ssh.Prohibited, "unsupported tunnel channel")
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = incoming.Reject(ssh.ResourceShortage, "tunnel channel limit reached")
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); defer func() { <-slots }(); h.handleChannel(ctx, incoming) }()
	}
}
