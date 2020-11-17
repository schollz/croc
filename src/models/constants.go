package models

import (
	"context"
	"fmt"
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
	DEFAULT_RELAY, _ = lookupIP(DEFAULT_RELAY)
	DEFAULT_RELAY += ":" + DEFAULT_PORT
	DEFAULT_RELAY6, _ = lookupIP(DEFAULT_RELAY6)
	DEFAULT_RELAY6 += "[" + DEFAULT_RELAY6 + "]:" + DEFAULT_PORT
	// iprecords, _ := lookupIP(DEFAULT_RELAY)
	// for _, ip := range iprecords {
	// 	DEFAULT_RELAY = ip.String() + ":" + DEFAULT_PORT
	// }
	// iprecords, _ = lookupIP(DEFAULT_RELAY6)
	// for _, ip := range iprecords {
	// 	DEFAULT_RELAY6 = "[" + ip.String() + "]:" + DEFAULT_PORT
	// }
}

func lookupIP(address string) (ipaddress string, err error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
			}
			return d.DialContext(ctx, "udp", "8.8.8.8:53")
		},
	}
	ip, err := r.LookupHost(context.Background(), address)
	if err != nil {
		fmt.Println(err)
		return
	}
	ipaddress = ip[0]
	return
}
