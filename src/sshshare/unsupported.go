//go:build croc_no_tailcat || (!linux && !windows && !darwin && !freebsd && !openbsd)

// Package sshshare provides croc's collaborative terminal feature. This file
// preserves the CLI and library surface in builds that intentionally omit the
// Tailcat transport or target a platform Tailcat does not support.
package sshshare

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

var errUnsupported = errors.New("croc ssh is not supported in this build")

var ErrDetached = errors.New("detached from shared SSH terminal")

type Role string

const (
	RoleReadWrite Role = "read-write"
	RoleReadOnly  Role = "read-only"
)

type Transport string

const (
	TransportTailcat Transport = "tailcat"
	TransportRelay   Transport = "relay"
)

type TransportMode string

const (
	TransportModeAuto    TransportMode = "auto"
	TransportModeTailcat TransportMode = "tailcat"
	TransportModeRelay   TransportMode = "relay"
)

type WindowSize struct {
	Width  int
	Height int
}

type HostEvent struct {
	Role      Role
	Connected bool
	Clients   int
}

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

type Host struct{}

func StartHost(context.Context, HostConfig) (*Host, error) { return nil, errUnsupported }
func (*Host) Code(Role) string                             { return "" }
func (*Host) Relay(Role) string                            { return "" }
func (*Host) AttachLocal(context.Context, io.Reader, io.Writer, WindowSize, <-chan WindowSize) error {
	return errUnsupported
}
func (*Host) AttachLocalTerminal(context.Context, *os.File, io.Writer) error { return errUnsupported }
func (*Host) Done() <-chan struct{}                                          { return nil }
func (*Host) Wait() error                                                    { return errUnsupported }
func (*Host) Close() error                                                   { return nil }

type JoinEvent struct {
	State     string
	Role      Role
	Transport Transport
	Attempt   int
	Err       error
}

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

func Join(context.Context, ClientConfig) error { return errUnsupported }
