// Package sshshare implements croc's authenticated, reconnectable SSH sharing
// protocol and shared-terminal lifecycle.
package sshshare

import (
	"errors"
	"io"
	"os"
	"time"
)

// ErrDetached indicates that the local user detached while leaving the shared
// shell and remote participants running.
var ErrDetached = errors.New("detached from shared SSH terminal")

// Role is the authority granted by an SSH invitation.
type Role string

const (
	RoleReadWrite Role = "read-write"
	RoleReadOnly  Role = "read-only"
)

// Transport identifies the terminal data path selected after PAKE.
type Transport string

const (
	TransportTailcat Transport = "tailcat"
	TransportRelay   Transport = "relay"
)

// TransportMode controls which terminal transport a guest may use.
type TransportMode string

const (
	TransportModeAuto    TransportMode = "auto"
	TransportModeTailcat TransportMode = "tailcat"
	TransportModeRelay   TransportMode = "relay"
)

// JoinState describes one phase of a guest connection.
type JoinState string

const (
	JoinStateConnecting   JoinState = "connecting"
	JoinStateConnected    JoinState = "connected"
	JoinStateDisconnected JoinState = "disconnected"
	JoinStateReconnecting JoinState = "reconnecting"
)

// JoinEvent describes guest-visible connection state. Attempt is one for the
// initial connection and for the first reconnect after a successful session;
// it increases for consecutive failed reconnects.
type JoinEvent struct {
	State     JoinState
	Role      Role
	Transport Transport
	Attempt   int
	Err       error
}

// ClientConfig configures a participant joining a shared terminal.
type ClientConfig struct {
	Code            string
	RelayAddress    string
	RelayPassword   string
	Curve           string
	Input           io.Reader
	Output          io.Writer
	ErrorOutput     io.Writer
	Terminal        *os.File
	InitialSize     WindowSize
	Reconnect       bool
	ReconnectWindow time.Duration
	TransportMode   TransportMode
	OnEvent         func(JoinEvent)
	Logf            func(string, ...any)
}

// HostEvent reports attachment lifecycle without exposing invitation codes or
// cryptographic key material.
type HostEvent struct {
	Role        Role
	Connected   bool
	Attachments int
}

// HostConfig configures a shared SSH terminal host. AuthorizationTTL limits
// the time between a successful PAKE exchange and the first SSH connection; a
// zero value uses the secure default.
type HostConfig struct {
	ReadWriteCode    string
	ReadOnlyCode     string
	RelayAddress     string
	RelayPassword    string
	Command          []string
	Directory        string
	InitialSize      WindowSize
	AuthorizationTTL time.Duration
	OnEvent          func(HostEvent)
	Logf             func(string, ...any)
}

// WindowSize is a terminal size in character cells.
type WindowSize struct {
	Width  int
	Height int
}
