package models

import (
	"context"
	"net"
	"time"
)

// TCP_BUFFER_SIZE is the maximum packet size
const TCP_BUFFER_SIZE = 1024 * 64

// DEFAULT_RELAY is the default relay used (can be set using --relay)
var (
	DEFAULT_RELAY      = "croc.schollz.com"
	DEFAULT_RELAY6     = "croc6.schollz.com"
	DEFAULT_PORT       = "9009"
	DEFAULT_PASSPHRASE = "pass123"
)

func init() {
	var err error
	DEFAULT_RELAY, err = lookupIP(DEFAULT_RELAY)
	if err == nil {
		DEFAULT_RELAY += ":" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY = ""
	}
	DEFAULT_RELAY6, err = lookupIP(DEFAULT_RELAY6)
	if err == nil {
		DEFAULT_RELAY6 = "[" + DEFAULT_RELAY6 + "]:" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY6 = ""
	}
}

func lookupIP(address string) (ipaddress string, err error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
			}
			return d.DialContext(ctx, "udp", "1.1.1.1:53")
		},
	}
	ip, err := r.LookupHost(context.Background(), address)
	if err != nil {
		return
	}
	ipaddress = ip[0]
	return
}
