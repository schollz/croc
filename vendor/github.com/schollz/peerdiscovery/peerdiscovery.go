package peerdiscovery

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
)

var address = "239.0.0.0:9999"

type Discovered struct {
	Address string
	Payload []byte
}

type Settings struct {
	Limit            int
	Port             string
	MulticastAddress string
	Payload          []byte
	Delay            time.Duration
	TimeLimit        time.Duration
}

type PeerDiscovery struct {
	settings Settings
	localIP  string
	received map[string][]byte
	sync.RWMutex
}

func New(settings ...Settings) (p *PeerDiscovery) {
	p = new(PeerDiscovery)
	p.Lock()
	defer p.Unlock()
	if len(settings) > 0 {
		p.settings = settings[0]
	}
	// defaults
	if p.settings.Port == "" {
		p.settings.Port = "9999"
	}
	if p.settings.MulticastAddress == "" {
		p.settings.MulticastAddress = "239.0.0.0"
	}
	if len(p.settings.Payload) == 0 {
		p.settings.Payload = []byte("hi")
	}
	if p.settings.Delay == time.Duration(0) {
		p.settings.Delay = 1 * time.Second
	}
	if p.settings.TimeLimit == time.Duration(0) {
		p.settings.TimeLimit = 10 * time.Second
	}
	p.localIP = GetLocalIP()
	p.received = make(map[string][]byte)
	return
}

func (p *PeerDiscovery) Discover() (discoveries []Discovered, err error) {
	p.RLock()
	address := p.settings.MulticastAddress + ":" + p.settings.Port
	payload := p.settings.Payload
	tickerDuration := p.settings.Delay
	timeLimit := p.settings.TimeLimit
	p.RUnlock()

	conn, err := newBroadcast(address)
	if err != nil {
		return
	}
	defer conn.Close()

	go p.listen()
	ticker := time.NewTicker(tickerDuration)
	defer ticker.Stop()
	start := time.Now()
	for t := range ticker.C {
		exit := false
		p.Lock()
		if len(p.received) >= p.settings.Limit && p.settings.Limit > 0 {
			exit = true
		}
		p.Unlock()
		conn.Write(payload)
		if exit || t.Sub(start) > timeLimit {
			break
		}
	}

	// send out broadcast that is finished
	conn2, err := newBroadcast(address)
	if err != nil {
		return
	}
	defer conn2.Close()
	conn2.Write(payload)

	p.Lock()
	discoveries = make([]Discovered, len(p.received))
	i := 0
	for ip := range p.received {
		discoveries[i] = Discovered{
			Address: ip,
			Payload: p.received[ip],
		}
		i++
	}
	p.Unlock()
	return
}

// newBroadcast creates a new UDP multicast connection on which to broadcast
func newBroadcast(address string) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

const (
	maxDatagramSize = 8192
)

// Listen binds to the UDP address and port given and writes packets received
// from that address to a buffer which is passed to a hander
func (p *PeerDiscovery) listen() (recievedBytes []byte, err error) {
	p.RLock()
	address := p.settings.MulticastAddress + ":" + p.settings.Port
	currentIP := p.localIP
	p.RUnlock()

	// Parse the string address
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return
	}

	// Open up a connection
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadBuffer(maxDatagramSize)

	// Loop forever reading from the socket
	for {
		buffer := make([]byte, maxDatagramSize)
		numBytes, src, err2 := conn.ReadFromUDP(buffer)
		if err2 != nil {
			err = errors.Wrap(err2, "could not read from udp")
			return
		}

		// log.Println(hex.Dump(buffer[:numBytes]))
		// log.Println(string(buffer))

		if src.IP.String() == currentIP {
			continue
		}
		// log.Println(numBytes, "bytes read from", src)

		p.Lock()
		if _, ok := p.received[src.IP.String()]; !ok {
			p.received[src.IP.String()] = buffer[:numBytes]
		}
		if len(p.received) >= p.settings.Limit && p.settings.Limit > 0 {
			p.Unlock()
			break
		}
		p.Unlock()
	}

	return
}
