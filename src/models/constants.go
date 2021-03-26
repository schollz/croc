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
	DEFAULT_RELAY, err = lookupIPs(DEFAULT_RELAY)
	if err == nil {
		DEFAULT_RELAY += ":" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY = ""
	}
	DEFAULT_RELAY6, err = lookupIPs(DEFAULT_RELAY6)
	if err == nil {
		DEFAULT_RELAY6 = "[" + DEFAULT_RELAY6 + "]:" + DEFAULT_PORT
	} else {
		DEFAULT_RELAY6 = ""
	}
}

func lookupIPs(address string) (ipaddress string, err error) {
	var publicDns = []string{"1.1.1.1", "8.8.8.8", "8.8.4.4", "1.0.0.1", "8.26.56.26", "208.67.222.222", "208.67.220.220"}
	result := make(chan string, len(publicDns)+1)
	go func() {
		ips, _ := net.LookupIP(address)
		for _, ip := range ips {
			result <- ip.String()
		}
		result <- ""
	}()
	for _, dns := range publicDns {
		go func(dns string) {
			s, _ := lookupIP(address, dns)
			result <- s
		}(dns)
	}
	for i := 0; i < len(publicDns); i++ {
		ipaddress = <-result
		if ipaddress != "" {
			return
		}
	}
	return
}

func lookupIP(address, dns string) (ipaddress string, err error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(1000),
			}
			return d.DialContext(ctx, "udp", dns+":53")
		},
	}
	ip, err := r.LookupHost(context.Background(), address)
	if err != nil {
		return
	}
	ipaddress = ip[0]
	return
}
