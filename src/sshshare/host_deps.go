//go:build !croc_no_tailcat && (linux || windows || darwin || freebsd || openbsd)

package sshshare

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"github.com/schollz/croc/v11/internal/tailcat"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/tcp"
	gossh "golang.org/x/crypto/ssh"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

type sshConnServer interface {
	HandleConn(net.Conn)
	Close() error
}

type hostTransport interface {
	AddAllowedClient(key.NodePublic)
	RemoveAllowedClient(key.NodePublic)
	Close() error
}

type hostDeps struct {
	startTerminal  func(context.Context, []string, string, WindowSize) (*terminalHub, error)
	generateSigner func() (gossh.Signer, error)
	newSSHServer   func(*terminalHub, gossh.Signer, Role, func(Role) func()) sshConnServer
	startTransport func(context.Context, HostConfig, func(uint16) func(net.Conn)) (hostTransport, string, error)
	connect        func(context.Context, string, string, string, time.Duration) (*comm.Comm, error)
	now            func() time.Time
}

func (d hostDeps) withDefaults() hostDeps {
	if d.startTerminal == nil {
		d.startTerminal = startTerminal
	}
	if d.generateSigner == nil {
		d.generateSigner = generateHostSigner
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
		d.connect = func(ctx context.Context, relay, password, room string, timeout time.Duration) (*comm.Comm, error) {
			connection, _, _, _, err := tcp.ConnectToTCPServerControlContext(ctx, relay, password, room, timeout)
			return connection, err
		}
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
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
