package server

import (
	"net"
)

func listAddresses() (addresses []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}

	addresses = make([]string, 0, len(ifaces))

	for _, iface := range ifaces {
		ifAddrs, _ := iface.Addrs()
		for _, ifAddr := range ifAddrs {
			switch v := ifAddr.(type) {
			case *net.IPNet:
				addresses = append(addresses, v.IP.String())
			case *net.IPAddr:
				addresses = append(addresses, v.IP.String())
			}
		}
	}

	return addresses
}
