package models

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"

	"github.com/schollz/croc/v10/src/utils"
)

// TCP_BUFFER_SIZE is the maximum packet size
const TCP_BUFFER_SIZE = 1024 * 64

// DEFAULT_RELAY is the default relay used (can be set using --relay)
var (
	DEFAULT_RELAY      = "croc.schollz.com"
	DEFAULT_RELAY6     = "croc6.schollz.com"
	DEFAULT_PORT       = "9009"
	DEFAULT_PASSPHRASE = "pass123"
	INTERNAL_DNS       = false
)

// publicDNS are servers to be queried if a local lookup fails
var publicDNS = []string{
	"1.0.0.1",                // Cloudflare
	"1.1.1.1",                // Cloudflare
	"[2606:4700:4700::1111]", // Cloudflare
	"[2606:4700:4700::1001]", // Cloudflare
	"8.8.4.4",                // Google
	"8.8.8.8",                // Google
	"[2001:4860:4860::8844]", // Google
	"[2001:4860:4860::8888]", // Google
	"9.9.9.9",                // Quad9
	"149.112.112.112",        // Quad9
	"[2620:fe::fe]",          // Quad9
	"[2620:fe::fe:9]",        // Quad9
	"8.26.56.26",             // Comodo
	"8.20.247.20",            // Comodo
	"208.67.220.220",         // Cisco OpenDNS
	"208.67.222.222",         // Cisco OpenDNS
	"[2620:119:35::35]",      // Cisco OpenDNS
	"[2620:119:53::53]",      // Cisco OpenDNS
}

func getConfigFile(requireValidPath bool) (fname string, err error) {
	configFile, err := utils.GetConfigDir(requireValidPath)
	if err != nil {
		return
	}
	fname = path.Join(configFile, "internal-dns")
	return
}

func init() {
	doRemember := false
	for _, flag := range os.Args {
		if flag == "--internal-dns" {
			INTERNAL_DNS = true
			break
		}
		if flag == "--remember" {
			doRemember = true
		}
	}
	if doRemember {
		// save in config file
		fname, err := getConfigFile(true)
		if err == nil {
			f, _ := os.Create(fname)
			f.Close()
		}
	}
	if !INTERNAL_DNS {
		fname, err := getConfigFile(false)
		if err == nil {
			INTERNAL_DNS = utils.Exists(fname)
		}
	}
	var err error
	var addr string
	addr, err = lookup(DEFAULT_RELAY)
	if err == nil {
		DEFAULT_RELAY = net.JoinHostPort(addr, DEFAULT_PORT)
	} else {
		DEFAULT_RELAY = ""
	}
	addr, err = lookup(DEFAULT_RELAY6)
	if err == nil {
		DEFAULT_RELAY6 = net.JoinHostPort(addr, DEFAULT_PORT)
	} else {
		DEFAULT_RELAY6 = ""
	}
}

// Resolve a hostname to an IP address using DNS.
func lookup(address string) (ipaddress string, err error) {
	if !INTERNAL_DNS {
		return localLookupIP(address)
	}
	type Result struct {
		s   string
		err error
	}
	result := make(chan Result, len(publicDNS))
	for _, dns := range publicDNS {
		go func(dns string) {
			var r Result
			r.s, r.err = remoteLookupIP(address, dns)
			result <- r
		}(dns)
	}
	for i := 0; i < len(publicDNS); i++ {
		ipaddress = (<-result).s
		if ipaddress != "" {
			return
		}
	}
	err = fmt.Errorf("failed to resolve %s: all DNS servers exhausted", address)
	return
}

// localLookupIP returns a host's IP address based on the local resolver.
func localLookupIP(address string) (ipaddress string, err error) {
	ip, err := net.LookupHost(address)
	if err != nil {
		return
	}
	ipaddress = ip[0]
	return
}

// remoteLookupIP returns a host's IP address based on a remote DNS server.
func remoteLookupIP(address, dns string) (ipaddress string, err error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := new(net.Dialer)
			return d.DialContext(ctx, network, dns+":53")
		},
	}
	ip, err := r.LookupHost(context.Background(), address)
	if err != nil {
		return
	}
	ipaddress = ip[0]
	return
}
