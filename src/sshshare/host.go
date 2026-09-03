//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/publicrelay"
	gossh "golang.org/x/crypto/ssh"
	"tailscale.com/types/key"
)

const (
	defaultAuthorizationTTL = 30 * time.Second
	rendezvousRetry         = 250 * time.Millisecond
)

type roleGrant struct {
	clientKey key.NodePublic
	role      Role
	expiresAt time.Time
}

type invitation struct {
	role       Role
	port       uint16
	code       string
	components codephrase.SSHComponents
	relay      string
	sshServer  sshConnServer
}

// Host owns one shared shell, one persistent Tailcat server, and repeatable
// read-write/read-only rendezvous loops. Invitation holders may disconnect and
// authenticate again while the host remains alive.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	config HostConfig
	deps   hostDeps

	hub         *terminalHub
	server      hostTransport
	offer       string
	hostKey     gossh.Signer
	invitations map[Role]*invitation

	grantMu sync.Mutex
	grants  map[netip.Addr]roleGrant

	clientMu    sync.Mutex
	attachments int
	eventMu     sync.Mutex

	sessionMu sync.Mutex
	closing   bool
	sessionWG sync.WaitGroup

	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	done      chan struct{}
}

// StartHost starts a reconnectable shared terminal and its two invitation
// listeners. It returns after the Tailcat server is reachable, without waiting
// for a participant.
func StartHost(parent context.Context, config HostConfig) (*Host, error) {
	return startHostWithDeps(parent, config, hostDeps{})
}

func startHostWithDeps(parent context.Context, config HostConfig, deps hostDeps) (*Host, error) {
	if parent == nil {
		return nil, errors.New("SSH host context is required")
	}
	deps = deps.withDefaults()
	if config.RelayPassword == "" {
		return nil, errors.New("SSH relay password is required")
	}
	if config.AuthorizationTTL == 0 {
		config.AuthorizationTTL = defaultAuthorizationTTL
	} else if config.AuthorizationTTL < 0 {
		return nil, errors.New("SSH authorization TTL must not be negative")
	}
	if config.ReadWriteCode == "" {
		generated, err := codephrase.GenerateSSH()
		if err != nil {
			return nil, err
		}
		config.ReadWriteCode = generated
	}
	readWriteComponents, err := codephrase.ParseSSH(config.ReadWriteCode)
	if err != nil {
		return nil, fmt.Errorf("%s code: %w", RoleReadWrite, err)
	}
	if config.ReadOnlyCode == "" {
		for {
			generated, err := codephrase.GenerateSSH()
			if err != nil {
				return nil, err
			}
			parsed, err := codephrase.ParseSSH(generated)
			if err != nil {
				return nil, err
			}
			if parsed.RoomName != readWriteComponents.RoomName {
				config.ReadOnlyCode = generated
				break
			}
		}
	}
	readOnlyComponents, err := codephrase.ParseSSH(config.ReadOnlyCode)
	if err != nil {
		return nil, fmt.Errorf("%s code: %w", RoleReadOnly, err)
	}
	if readOnlyComponents.RoomName == readWriteComponents.RoomName {
		return nil, errors.New("read-write and read-only SSH codes must have different first two words")
	}

	invitations := map[Role]*invitation{
		RoleReadWrite: {
			role: RoleReadWrite, port: readWritePort,
			code: config.ReadWriteCode, components: readWriteComponents,
		},
		RoleReadOnly: {
			role: RoleReadOnly, port: readOnlyPort,
			code: config.ReadOnlyCode, components: readOnlyComponents,
		},
	}
	for _, invite := range invitations {
		relay, err := relayForCode(config.RelayAddress, invite.code)
		if err != nil {
			return nil, err
		}
		invite.relay = relay
	}

	ctx, cancel := context.WithCancel(parent)
	hub, err := deps.startTerminal(ctx, config.Command, config.Directory, config.InitialSize)
	if err != nil {
		cancel()
		return nil, err
	}
	signer, err := deps.generateSigner()
	if err != nil {
		_ = hub.Close()
		cancel()
		return nil, err
	}

	h := &Host{
		ctx:         ctx,
		cancel:      cancel,
		config:      config,
		deps:        deps,
		hub:         hub,
		hostKey:     signer,
		invitations: invitations,
		grants:      make(map[netip.Addr]roleGrant),
		done:        make(chan struct{}),
	}
	for _, invite := range invitations {
		invite.sshServer = deps.newSSHServer(hub, signer, invite.role, h.beginAttachment)
	}

	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()
	server, offer, err := deps.startTransport(startupCtx, config, h.handlerForPort)
	if err != nil {
		if server != nil {
			_ = server.Close()
		}
		_ = hub.Close()
		cancel()
		return nil, err
	}
	h.server = server
	h.offer = offer

	h.wg.Add(1)
	go h.serveGrantExpiry()
	for _, invite := range invitations {
		h.wg.Add(1)
		go h.serveRendezvous(invite)
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-hub.Done():
		}
		_ = h.Close()
	}()
	return h, nil
}

func (h *Host) serveGrantExpiry() {
	defer h.wg.Done()
	interval := min(max(h.config.AuthorizationTTL/2, 10*time.Millisecond), time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			h.expireGrants(now)
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *Host) expireGrants(now time.Time) {
	h.grantMu.Lock()
	defer h.grantMu.Unlock()
	for address, grant := range h.grants {
		if !now.Before(grant.expiresAt) {
			delete(h.grants, address)
			if h.server != nil {
				h.server.RemoveAllowedClient(grant.clientKey)
			}
		}
	}
}

func (h *Host) revokeAllGrants() {
	h.grantMu.Lock()
	defer h.grantMu.Unlock()
	for address, grant := range h.grants {
		delete(h.grants, address)
		if h.server != nil {
			h.server.RemoveAllowedClient(grant.clientKey)
		}
	}
}

func relayForCode(explicit, code string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	relays := publicrelay.Relays()
	index, err := codephrase.RelayIndex(code, len(relays))
	if err != nil {
		return "", err
	}
	return relays[index], nil
}

func (h *Host) serveRendezvous(invite *invitation) {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		connection, err := h.deps.connect(
			h.ctx, invite.relay, h.config.RelayPassword, invite.components.RoomName, 10*time.Second,
		)
		if err == nil {
			stopClose := context.AfterFunc(h.ctx, connection.Close)
			var handedOff bool
			handedOff, err = h.authorizeParticipant(connection, invite)
			if !handedOff {
				connection.Close()
			}
			stopClose()
		}
		if err != nil && h.ctx.Err() == nil && h.config.Logf != nil {
			h.config.Logf("SSH %s rendezvous: %v", invite.role, err)
		}
		timer := time.NewTimer(rendezvousRetry)
		select {
		case <-timer.C:
		case <-h.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (h *Host) authorizeParticipant(connection *comm.Comm, invite *invitation) (bool, error) {
	encryptionKey, deadline, err := hostPAKE(connection, invite.components)
	if err != nil {
		return false, err
	}
	request, err := receiveAuthorizationRequest(connection, encryptionKey, deadline)
	if err != nil {
		return false, err
	}
	offer := sshOffer{
		TailcatAddress: h.offer,
		SSHHostKey:     h.hostKey.PublicKey().Marshal(),
		Port:           invite.port,
		Role:           invite.role,
		Transport:      request.transport,
	}
	if request.transport == TransportRelay {
		if err = invite.sshServer.AddClientAuth(request.clientAuth, h.now().Add(h.config.AuthorizationTTL)); err != nil {
			return false, err
		}
		if err = sendOffer(connection, encryptionKey, offer, deadline); err != nil {
			invite.sshServer.RevokeClientAuth(request.clientAuth)
			return false, err
		}
		if err = connection.Connection().SetDeadline(time.Time{}); err != nil {
			invite.sshServer.RevokeClientAuth(request.clientAuth)
			return false, fmt.Errorf("clear SSH relay deadline: %w", err)
		}
		h.serveRelaySSH(connection, invite)
		return true, nil
	}

	clientKey := request.clientKey
	address := tailcat.AddrForNodeKey(clientKey)
	expiresAt := h.now().Add(h.config.AuthorizationTTL)
	if err = invite.sshServer.AddClientAuth(request.clientAuth, expiresAt); err != nil {
		return false, err
	}
	if !h.addGrant(address, roleGrant{
		clientKey: clientKey,
		role:      invite.role,
		expiresAt: expiresAt,
	}) {
		invite.sshServer.RevokeClientAuth(request.clientAuth)
		if err := h.ctx.Err(); err != nil {
			return false, err
		}
		return false, errors.New("SSH host is closed")
	}
	if err = sendOffer(connection, encryptionKey, offer, deadline); err != nil {
		h.revokeGrant(address, clientKey)
		invite.sshServer.RevokeClientAuth(request.clientAuth)
	}
	return false, err
}

func (h *Host) addGrant(address netip.Addr, grant roleGrant) bool {
	// Use the same lifecycle lock as beginSession so Close either observes and
	// revokes this grant or prevents it from being installed.
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if h.closing {
		return false
	}
	h.grantMu.Lock()
	defer h.grantMu.Unlock()
	h.grants[address] = grant
	h.server.AddAllowedClient(grant.clientKey)
	return true
}

func (h *Host) serveRelaySSH(connection *comm.Comm, invite *invitation) {
	stopClose := context.AfterFunc(h.ctx, connection.Close)
	if !h.beginSession() {
		stopClose()
		connection.Close()
		return
	}
	go func() {
		defer h.sessionWG.Done()
		defer stopClose()
		defer connection.Close()
		invite.sshServer.HandleConn(connection.Connection())
	}()
}

func (h *Host) handlerForPort(port uint16) func(net.Conn) {
	var invite *invitation
	for _, candidate := range h.invitations {
		if candidate.port == port {
			invite = candidate
			break
		}
	}
	if invite == nil {
		return nil
	}
	return func(connection net.Conn) {
		if !h.beginSession() {
			_ = connection.Close()
			return
		}
		defer h.sessionWG.Done()
		address, ok := remoteIP(connection.RemoteAddr())
		if !ok {
			_ = connection.Close()
			return
		}
		_, ok = h.consumeGrant(address, invite.role)
		if !ok {
			_ = connection.Close()
			return
		}
		invite.sshServer.HandleConn(connection)
	}
}

func (h *Host) beginSession() bool {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if h.closing {
		return false
	}
	h.sessionWG.Add(1)
	return true
}

func remoteIP(address net.Addr) (netip.Addr, bool) {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		ip, ok := netip.AddrFromSlice(tcpAddress.IP)
		return ip.Unmap(), ok
	}
	parsed, err := netip.ParseAddrPort(address.String())
	if err != nil {
		return netip.Addr{}, false
	}
	return parsed.Addr().Unmap(), true
}

// consumeGrant makes each PAKE authorization good for exactly one SSH
// transport connection. A reconnect therefore has to authenticate the
// invitation again even though one Join call keeps a stable Tailcat identity.
func (h *Host) consumeGrant(address netip.Addr, role Role) (key.NodePublic, bool) {
	h.grantMu.Lock()
	grant, ok := h.grants[address]
	expired := ok && h.now().After(grant.expiresAt)
	if ok && (grant.role != role || expired) {
		if expired {
			delete(h.grants, address)
		}
		ok = false
	}
	if ok {
		delete(h.grants, address)
	}
	if (ok || expired) && !grant.clientKey.IsZero() && h.server != nil {
		h.server.RemoveAllowedClient(grant.clientKey)
	}
	h.grantMu.Unlock()
	if !ok {
		return key.NodePublic{}, false
	}
	return grant.clientKey, true
}

func (h *Host) now() time.Time {
	if h.deps.now != nil {
		return h.deps.now()
	}
	return time.Now()
}

func (h *Host) revokeGrant(address netip.Addr, clientKey key.NodePublic) {
	h.grantMu.Lock()
	defer h.grantMu.Unlock()
	if grant, ok := h.grants[address]; ok && grant.clientKey == clientKey {
		delete(h.grants, address)
		if h.server != nil {
			h.server.RemoveAllowedClient(clientKey)
		}
	}
}

func (h *Host) participantEvent(role Role, connected bool) {
	h.clientMu.Lock()
	if connected {
		h.attachments++
	} else if h.attachments > 0 {
		h.attachments--
	}
	event := HostEvent{Role: role, Connected: connected, Attachments: h.attachments}
	h.clientMu.Unlock()
	if h.config.OnEvent != nil {
		h.eventMu.Lock()
		defer h.eventMu.Unlock()
		h.config.OnEvent(event)
	}
}

func (h *Host) beginAttachment(role Role) func() {
	h.sessionMu.Lock()
	if h.closing {
		h.sessionMu.Unlock()
		return nil
	}
	h.sessionWG.Add(1)
	h.sessionMu.Unlock()
	h.participantEvent(role, true)
	return sync.OnceFunc(func() {
		h.participantEvent(role, false)
		h.sessionWG.Done()
	})
}

// Code returns the invitation for role.
func (h *Host) Code(role Role) string {
	if invite := h.invitations[role]; invite != nil {
		return invite.code
	}
	return ""
}

// Relay returns the rendezvous relay selected for role.
func (h *Host) Relay(role Role) string {
	if invite := h.invitations[role]; invite != nil {
		return invite.relay
	}
	return ""
}

// AttachLocal attaches the host's own terminal to the shared session.
func (h *Host) AttachLocal(ctx context.Context, input io.Reader, output io.Writer, size WindowSize, resizes <-chan WindowSize) error {
	return h.hub.Attach(ctx, input, output, true, size, resizes)
}

// Done closes when the host or its shared shell stops.
func (h *Host) Done() <-chan struct{} { return h.done }

// Wait waits for shutdown and returns the shared shell's terminal error.
func (h *Host) Wait() error {
	<-h.done
	return h.hub.Err()
}

// Close revokes both invitations, disconnects participants, and stops the
// shared shell.
func (h *Host) Close() error {
	h.closeOnce.Do(func() {
		h.sessionMu.Lock()
		h.closing = true
		h.sessionMu.Unlock()
		h.cancel()
		h.revokeAllGrants()
		for _, invite := range h.invitations {
			if invite.sshServer != nil {
				h.closeErr = errors.Join(h.closeErr, invite.sshServer.Close())
			}
		}
		if h.server != nil {
			h.closeErr = errors.Join(h.closeErr, h.server.Close())
		}
		if h.hub != nil {
			h.closeErr = errors.Join(h.closeErr, h.hub.Close())
		}
		h.wg.Wait()
		h.sessionWG.Wait()
		close(h.done)
	})
	return h.closeErr
}
