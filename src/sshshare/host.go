//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"github.com/schollz/croc/v11/src/tcp"
	gossh "golang.org/x/crypto/ssh"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

const (
	readWritePort    = 22
	readOnlyPort     = 23
	defaultAccessTTL = 12 * time.Hour
	rendezvousRetry  = 250 * time.Millisecond
)

// HostEvent reports participant lifecycle without exposing invitation codes or
// cryptographic key material.
type HostEvent struct {
	Role      Role
	Connected bool
	Clients   int
}

// HostConfig configures a shared SSH terminal host.
type HostConfig struct {
	ReadWriteCode string
	ReadOnlyCode  string
	RelayAddress  string
	RelayPassword string
	Command       []string
	Directory     string
	InitialSize   WindowSize
	AccessTTL     time.Duration
	OnEvent       func(HostEvent)
	Logf          func(string, ...any)
}

type roleGrant struct {
	clientKey key.NodePublic
	role      Role
	expiresAt time.Time
}

// Host owns one shared shell, one persistent Tailcat server, and repeatable
// read-write/read-only rendezvous loops. Invitation holders may disconnect and
// authenticate again while the host remains alive.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	config HostConfig

	hub        *terminalHub
	server     *tailcat.Server
	offer      string
	hostKey    gossh.Signer
	components map[Role]codephrase.SSHComponents
	codes      map[Role]string
	relays     map[Role]string
	sshServers map[Role]interface{ HandleConn(net.Conn) }

	grantMu sync.Mutex
	grants  map[netip.Addr]roleGrant

	clientMu sync.Mutex
	clients  int

	wg        sync.WaitGroup
	closeOnce sync.Once
	done      chan struct{}
}

// StartHost starts a reconnectable shared terminal and its two invitation
// listeners. It returns after the Tailcat server is reachable, without waiting
// for a participant.
func StartHost(parent context.Context, config HostConfig) (*Host, error) {
	if parent == nil {
		return nil, errors.New("SSH host context is required")
	}
	if config.RelayPassword == "" {
		return nil, errors.New("SSH relay password is required")
	}
	if config.AccessTTL <= 0 {
		config.AccessTTL = defaultAccessTTL
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

	components := map[Role]codephrase.SSHComponents{
		RoleReadWrite: readWriteComponents,
		RoleReadOnly:  readOnlyComponents,
	}
	codes := map[Role]string{
		RoleReadWrite: config.ReadWriteCode,
		RoleReadOnly:  config.ReadOnlyCode,
	}
	relays := make(map[Role]string, 2)
	for role, code := range codes {
		relay, err := relayForCode(config.RelayAddress, code)
		if err != nil {
			return nil, err
		}
		relays[role] = relay
	}

	ctx, cancel := context.WithCancel(parent)
	hub, err := startTerminal(ctx, config.Command, config.Directory, config.InitialSize)
	if err != nil {
		cancel()
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		_ = hub.Close()
		cancel()
		return nil, fmt.Errorf("generate ephemeral SSH host key: %w", err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		_ = hub.Close()
		cancel()
		return nil, fmt.Errorf("create SSH host signer: %w", err)
	}

	h := &Host{
		ctx:        ctx,
		cancel:     cancel,
		config:     config,
		hub:        hub,
		hostKey:    signer,
		components: components,
		codes:      codes,
		relays:     relays,
		grants:     make(map[netip.Addr]roleGrant),
		sshServers: make(map[Role]interface{ HandleConn(net.Conn) }),
		done:       make(chan struct{}),
	}
	h.sshServers[RoleReadWrite] = newSharedSSHServer(hub, signer, RoleReadWrite, h.participantEvent)
	h.sshServers[RoleReadOnly] = newSharedSSHServer(hub, signer, RoleReadOnly, h.participantEvent)

	// A non-empty allowlist makes the server fail closed before the first real
	// participant completes PAKE. The sentinel private key is discarded.
	sentinel := key.NewNode().Public()
	server := &tailcat.Server{
		Key:            key.NewNode(),
		AllowedClients: []key.NodePublic{sentinel},
		ServedTCPPorts: []filter.PortRange{
			{First: readWritePort, Last: readWritePort},
			{First: readOnlyPort, Last: readOnlyPort},
		},
		Logf: config.Logf,
	}
	server.OnTCP = h.handlerForPort
	h.server = server
	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startupCancel()
	if err = server.StartContext(startupCtx); err != nil {
		_ = hub.Close()
		cancel()
		return nil, fmt.Errorf("start SSH Tailcat server: %w", err)
	}
	h.offer = string(server.ConnBlob())

	for _, role := range []Role{RoleReadWrite, RoleReadOnly} {
		h.wg.Add(1)
		go h.serveRendezvous(role)
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

func (h *Host) serveRendezvous(role Role) {
	defer h.wg.Done()
	components := h.components[role]
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		connection, _, _, _, err := tcp.ConnectToTCPServerControl(
			h.relays[role], h.config.RelayPassword, components.RoomName, 10*time.Second,
		)
		if err == nil {
			stopClose := context.AfterFunc(h.ctx, connection.Close)
			var handedOff bool
			handedOff, err = h.authorizeParticipant(connection, components, role)
			if !handedOff {
				connection.Close()
			}
			stopClose()
		}
		if err != nil && h.ctx.Err() == nil && h.config.Logf != nil {
			h.config.Logf("SSH %s rendezvous: %v", role, err)
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

func (h *Host) authorizeParticipant(connection *comm.Comm, components codephrase.SSHComponents, role Role) (bool, error) {
	encryptionKey, err := hostPAKE(connection, components)
	if err != nil {
		return false, err
	}
	request, err := receiveAuthorizationRequest(connection, encryptionKey)
	if err != nil {
		return false, err
	}
	port, _ := role.port()
	offer := Offer{
		TailcatAddress: h.offer,
		SSHHostKey:     h.hostKey.PublicKey().Marshal(),
		Port:           port,
		Role:           role,
		Transport:      request.transport,
	}
	if request.transport == TransportRelay {
		if err = sendOffer(connection, encryptionKey, offer); err != nil {
			return false, err
		}
		if err = connection.Connection().SetDeadline(time.Time{}); err != nil {
			return false, fmt.Errorf("clear SSH relay deadline: %w", err)
		}
		h.serveRelaySSH(connection, role)
		return true, nil
	}

	clientKey := request.clientKey
	address := tailcat.AddrForNodeKey(clientKey)
	h.grantMu.Lock()
	h.grants[address] = roleGrant{
		clientKey: clientKey, role: role, expiresAt: time.Now().Add(h.config.AccessTTL),
	}
	h.grantMu.Unlock()
	h.server.AddAllowedClient(clientKey)
	if err = sendOffer(connection, encryptionKey, offer); err != nil {
		h.revokeGrant(address, clientKey)
	}
	return false, err
}

func (h *Host) serveRelaySSH(connection *comm.Comm, role Role) {
	stopClose := context.AfterFunc(h.ctx, connection.Close)
	go func() {
		defer stopClose()
		defer connection.Close()
		h.sshServers[role].HandleConn(connection.Connection())
	}()
}

func (h *Host) handlerForPort(port uint16) func(net.Conn) {
	role := Role("")
	switch port {
	case readWritePort:
		role = RoleReadWrite
	case readOnlyPort:
		role = RoleReadOnly
	default:
		return nil
	}
	return func(connection net.Conn) {
		address, ok := remoteIP(connection.RemoteAddr())
		if !ok {
			_ = connection.Close()
			return
		}
		clientKey, ok := h.consumeGrant(address, role)
		if !ok {
			_ = connection.Close()
			return
		}
		defer h.server.RemoveAllowedClient(clientKey)
		h.sshServers[role].HandleConn(connection)
	}
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
// invitation again and receives a fresh Tailcat key.
func (h *Host) consumeGrant(address netip.Addr, role Role) (key.NodePublic, bool) {
	h.grantMu.Lock()
	grant, ok := h.grants[address]
	expired := ok && time.Now().After(grant.expiresAt)
	if ok && (grant.role != role || expired) {
		if expired {
			delete(h.grants, address)
		}
		ok = false
	}
	if ok {
		delete(h.grants, address)
	}
	h.grantMu.Unlock()
	if !ok {
		if expired && !grant.clientKey.IsZero() && h.server != nil {
			h.server.RemoveAllowedClient(grant.clientKey)
		}
		return key.NodePublic{}, false
	}
	return grant.clientKey, true
}

func (h *Host) revokeGrant(address netip.Addr, clientKey key.NodePublic) {
	h.grantMu.Lock()
	if grant, ok := h.grants[address]; ok && grant.clientKey == clientKey {
		delete(h.grants, address)
	}
	h.grantMu.Unlock()
	if h.server != nil {
		h.server.RemoveAllowedClient(clientKey)
	}
}

func (h *Host) participantEvent(role Role, connected bool) {
	h.clientMu.Lock()
	if connected {
		h.clients++
	} else if h.clients > 0 {
		h.clients--
	}
	event := HostEvent{Role: role, Connected: connected, Clients: h.clients}
	h.clientMu.Unlock()
	if h.config.OnEvent != nil {
		h.config.OnEvent(event)
	}
}

// Code returns the invitation for role.
func (h *Host) Code(role Role) string { return h.codes[role] }

// Relay returns the rendezvous relay selected for role.
func (h *Host) Relay(role Role) string { return h.relays[role] }

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
	var result error
	h.closeOnce.Do(func() {
		h.cancel()
		if h.server != nil {
			result = h.server.Close()
		}
		if h.hub != nil {
			if err := h.hub.Close(); result == nil {
				result = err
			}
		}
		close(h.done)
	})
	return result
}
