package croc

import (
	"bytes"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Relay parameters
	RelayWebsocketPort string
	RelayTCPPorts      []string

	// Sender parameters
	CurveType string

	// Options for connecting to server
	PublicServerIP       string
	AddressTCPPorts      []string
	AddressWebsocketPort string
	Timeout              time.Duration
	LocalOnly            bool
	NoLocal              bool

	// Options for file transfering
	UseEncryption       bool
	UseCompression      bool
	AllowLocalDiscovery bool
	NoRecipientPrompt   bool
	ForceTCP            bool
	ForceWebsockets     bool
	Codephrase          string
}

// DefaultConfig returns the default config
func DefaultConfig() string {
	c := Config{}
	cr := Init(false)
	c.RelayWebsocketPort = cr.RelayWebsocketPort
	c.RelayTCPPorts = cr.RelayTCPPorts
	c.CurveType = cr.CurveType
	c.PublicServerIP = cr.Address
	c.AddressTCPPorts = cr.AddressTCPPorts
	c.AddressWebsocketPort = cr.AddressWebsocketPort
	c.Timeout = cr.Timeout
	c.LocalOnly = cr.LocalOnly
	c.NoLocal = cr.NoLocal
	c.UseEncryption = cr.UseEncryption
	c.UseCompression = cr.UseCompression
	c.AllowLocalDiscovery = cr.AllowLocalDiscovery
	c.NoRecipientPrompt = cr.NoRecipientPrompt
	c.ForceTCP = false
	c.ForceWebsockets = false
	c.Codephrase = ""
	buf := new(bytes.Buffer)
	toml.NewEncoder(buf).Encode(c)
	return buf.String()
}
