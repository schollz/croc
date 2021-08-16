package models

import (
	"context"
	"fmt"
	"net"
	"os"
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

// publicDns are servers to be queried if a local lookup fails
var publicDns = []string{
	"1.0.0.1",                // Cloudflare
	"1.1.1.1",                // Cloudflare
	"8.8.4.4",                // Google
	"8.8.8.8",                // Google
	"8.26.56.26",             // Comodo
	"208.67.220.220",         // Cisco OpenDNS
	"208.67.222.222",         // Cisco OpenDNS
	"[2001:4860:4860::8844]", // Google
	"[2001:4860:4860::8888]", // Google
}

func init() {
	for _, flag := range os.Args {
		if flag == "--internal-dns" {
			INTERNAL_DNS = true
			break
		}
	}
	var err error
	DEFAULT_RELAY, err = lookup(DEFAULT_RELAY)
	if err == nil {
		DEFAULT_RELAY += ":" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY = ""
	}
	DEFAULT_RELAY6, err = lookup(DEFAULT_RELAY6)
	if err == nil {
		DEFAULT_RELAY6 = "[" + DEFAULT_RELAY6 + "]:" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY6 = ""
	}
}

// Resolve a hostname to an IP address using DNS.
func lookup(address string) (ipaddress string, err error) {
	if !INTERNAL_DNS {
		return localLookupIP(address)
	}
	result := make(chan string, len(publicDns))
	for _, dns := range publicDns {
		go func(dns string) {
			s, err := remoteLookupIP(address, dns)
			if err == nil {
				result <- s
			}
		}(dns)
	}
	for i := 0; i < len(publicDns); i++ {
		ipaddress = <-result
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
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
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
