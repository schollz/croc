//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/tcp"
	gossh "golang.org/x/crypto/ssh"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

type sshConnServer interface {
	HandleConn(net.Conn)
	AddClientAuth([]byte, time.Time) error
	RevokeClientAuth([]byte)
	Close() error
}

type hostTransport interface {
	AddAllowedClient(key.NodePublic)
	RemoveAllowedClient(key.NodePublic)
	Close() error
}

type relaySession struct {
	connection *comm.Comm
	banner     string
	capability string
}

type hostDeps struct {
	startTerminal   func(context.Context, []string, string, WindowSize) (*terminalHub, error)
	generateSigner  func() (gossh.Signer, error)
	generateSSHCode func() (string, error)
	newSSHServer    func(*terminalHub, gossh.Signer, Role, func(Role) func()) sshConnServer
	startTransport  func(context.Context, HostConfig, func(uint16) func(net.Conn)) (hostTransport, string, error)
	connect         func(context.Context, string, string, string, time.Duration) (relaySession, error)
	connectData     func(context.Context, string, string, string, string, time.Duration) (*comm.Comm, error)
	legacyRelays    func() []string
	relayKey        func(string) string
	now             func() time.Time
}

func (d hostDeps) withDefaults() hostDeps {
	if d.startTerminal == nil {
		d.startTerminal = startTerminal
	}
	if d.generateSigner == nil {
		d.generateSigner = generateHostSigner
	}
	if d.generateSSHCode == nil {
		d.generateSSHCode = codephrase.GenerateSSH
	}
	if d.newSSHServer == nil {
		d.newSSHServer = func(hub *terminalHub, signer gossh.Signer, role Role, onAttach func(Role) func()) sshConnServer {
			return newSharedSSHServer(hub, signer, role, onAttach)
		}
	}
	if d.startTransport == nil {
		d.startTransport = startTailcatHostTransport
	}
	if d.connect == nil {
		d.connect = func(ctx context.Context, relay, password, room string, timeout time.Duration) (relaySession, error) {
			connection, banner, _, capability, err := tcp.ConnectToTCPServerControlContext(ctx, relay, password, room, timeout)
			return relaySession{
				connection: connection,
				banner:     banner,
				capability: capability,
			}, err
		}
	}
	if d.connectData == nil {
		d.connectData = func(ctx context.Context, relay, password, room, capability string, timeout time.Duration) (*comm.Comm, error) {
			connection, _, _, _, err := tcp.ConnectToTCPServerWithCapabilityContext(
				ctx, relay, password, room, capability, timeout,
			)
			return connection, err
		}
	}
	if d.legacyRelays == nil {
		d.legacyRelays = func() []string {
			return []string{models.DEFAULT_RELAY6, models.DEFAULT_RELAY}
		}
	}
	if d.relayKey == nil {
		d.relayKey = canonicalRelayKey
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
}

func canonicalRelayKey(address string) string {
	// The v11.1 default and the first newer public relay may be DNS aliases.
	// Resolve them before de-duplicating or the host can join its own room.
	resolved, err := net.ResolveTCPAddr("tcp", address)
	if err != nil || resolved.IP == nil {
		return strings.ToLower(address)
	}
	return resolved.String()
}

func generateHostSigner() (gossh.Signer, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral SSH host key: %w", err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		return nil, fmt.Errorf("create SSH host signer: %w", err)
	}
	return signer, nil
}

func startTailcatHostTransport(
	ctx context.Context,
	config HostConfig,
	handler func(uint16) func(net.Conn),
) (hostTransport, string, error) {
	// A non-empty allowlist makes the server fail closed before the first real
	// participant completes PAKE. The sentinel private key is discarded.
	server := &tailcat.Server{
		Key:            key.NewNode(),
		AllowedClients: []key.NodePublic{key.NewNode().Public()},
		ServedTCPPorts: []filter.PortRange{
			{First: readWritePort, Last: readWritePort},
			{First: readOnlyPort, Last: readOnlyPort},
		},
		OnTCP: handler,
		Logf:  config.Logf,
	}
	if err := server.StartContext(ctx); err != nil {
		_ = server.Close()
		return nil, "", fmt.Errorf("start SSH Tailcat server: %w", err)
	}
	return server, string(server.ConnBlob()), nil
}
