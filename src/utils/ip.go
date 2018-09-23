package utils

import (
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
)

func PublicIP() (ip string, err error) {
	resp, err := http.Get("https://canhazip.com")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		ip = strings.TrimSpace(string(bodyBytes))
	}
	return
}

// Get preferred outbound ip of this machine
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}
