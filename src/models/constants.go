package models

import "net"

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
	iprecords, _ := net.LookupIP(DEFAULT_RELAY)
	for _, ip := range iprecords {
		DEFAULT_RELAY = ip.String() + ":" + DEFAULT_PORT
	}
	iprecords, _ = net.LookupIP(DEFAULT_RELAY6)
	for _, ip := range iprecords {
		DEFAULT_RELAY6 = "[" + ip.String() + "]:" + DEFAULT_PORT
	}
}
