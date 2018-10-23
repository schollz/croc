package croc

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/schollz/croc/src/utils"
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

func defaultConfig() Config {
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
	return c
}

func SaveDefaultConfig() error {
	homedir, err := homedir.Dir()
	if err != nil {
		return err
	}
	os.MkdirAll(path.Join(homedir, ".config", "croc"), 0755)
	c := defaultConfig()
	buf := new(bytes.Buffer)
	toml.NewEncoder(buf).Encode(c)
	confTOML := buf.String()
	err = ioutil.WriteFile(path.Join(homedir, ".config", "croc", "config.toml"), []byte(confTOML), 0644)
	if err == nil {
		fmt.Printf("Default config file written at '%s'\r\n", filepath.Clean(path.Join(homedir, ".config", "croc", "config.toml")))
	}
	return err
}

// LoadConfig will override parameters
func (cr *Croc) LoadConfig() (err error) {
	homedir, err := homedir.Dir()
	if err != nil {
		return err
	}
	pathToConfig := path.Join(homedir, ".config", "croc", "config.toml")
	if !utils.Exists(pathToConfig) {
		// ignore if doesn't exist
		return nil
	}

	var c Config
	_, err = toml.DecodeFile(pathToConfig, &c)
	if err != nil {
		return
	}

	cDefault := defaultConfig()
	// only load if things are different than defaults
	// just in case the CLI parameters are used
	if c.RelayWebsocketPort != cDefault.RelayWebsocketPort && cr.RelayWebsocketPort == cDefault.RelayWebsocketPort {
		cr.RelayWebsocketPort = c.RelayWebsocketPort
		fmt.Printf("loaded RelayWebsocketPort from config\n")
	}
	if !slicesEqual(c.RelayTCPPorts, cDefault.RelayTCPPorts) && slicesEqual(cr.RelayTCPPorts, cDefault.RelayTCPPorts) {
		cr.RelayTCPPorts = c.RelayTCPPorts
		fmt.Printf("loaded RelayTCPPorts from config\n")
	}
	if c.CurveType != cDefault.CurveType && cr.CurveType == cDefault.CurveType {
		cr.CurveType = c.CurveType
		fmt.Printf("loaded CurveType from config\n")
	}
	if c.PublicServerIP != cDefault.PublicServerIP && cr.Address == cDefault.PublicServerIP {
		cr.Address = c.PublicServerIP
		fmt.Printf("loaded Address from config\n")
	}
	if !slicesEqual(c.AddressTCPPorts, cDefault.AddressTCPPorts) {
		cr.AddressTCPPorts = c.AddressTCPPorts
		fmt.Printf("loaded AddressTCPPorts from config\n")
	}
	if c.AddressWebsocketPort != cDefault.AddressWebsocketPort && cr.AddressWebsocketPort == cDefault.AddressWebsocketPort {
		cr.AddressWebsocketPort = c.AddressWebsocketPort
		fmt.Printf("loaded AddressWebsocketPort from config\n")
	}
	if c.Timeout != cDefault.Timeout && cr.Timeout == cDefault.Timeout {
		cr.Timeout = c.Timeout
		fmt.Printf("loaded Timeout from config\n")
	}
	if c.LocalOnly != cDefault.LocalOnly && cr.LocalOnly == cDefault.LocalOnly {
		cr.LocalOnly = c.LocalOnly
		fmt.Printf("loaded LocalOnly from config\n")
	}
	if c.NoLocal != cDefault.NoLocal && cr.NoLocal == cDefault.NoLocal {
		cr.NoLocal = c.NoLocal
		fmt.Printf("loaded NoLocal from config\n")
	}
	if c.UseEncryption != cDefault.UseEncryption && cr.UseEncryption == cDefault.UseEncryption {
		cr.UseEncryption = c.UseEncryption
		fmt.Printf("loaded UseEncryption from config\n")
	}
	if c.UseCompression != cDefault.UseCompression && cr.UseCompression == cDefault.UseCompression {
		cr.UseCompression = c.UseCompression
		fmt.Printf("loaded UseCompression from config\n")
	}
	if c.AllowLocalDiscovery != cDefault.AllowLocalDiscovery && cr.AllowLocalDiscovery == cDefault.AllowLocalDiscovery {
		cr.AllowLocalDiscovery = c.AllowLocalDiscovery
		fmt.Printf("loaded AllowLocalDiscovery from config\n")
	}
	if c.NoRecipientPrompt != cDefault.NoRecipientPrompt && cr.NoRecipientPrompt == cDefault.NoRecipientPrompt {
		cr.NoRecipientPrompt = c.NoRecipientPrompt
		fmt.Printf("loaded NoRecipientPrompt from config\n")
	}
	if c.ForceWebsockets {
		cr.ForceSend = 1
	}
	if c.ForceTCP {
		cr.ForceSend = 2
	}
	if c.Codephrase != cDefault.Codephrase && cr.Codephrase == cDefault.Codephrase {
		cr.Codephrase = c.Codephrase
		fmt.Printf("loaded Codephrase from config\n")
	}
	return
}

// slicesEqual checcks if two slices are equal
// from https://stackoverflow.com/a/15312097
func slicesEqual(a, b []string) bool {
	// If one is nil, the other must also be nil.
	if (a == nil) != (b == nil) {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
