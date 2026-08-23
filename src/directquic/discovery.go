package directquic

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

const probeSize = 8 + 32 + 1 + 16 + 16

func ValidateSTUNServer(value string) error {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "off") {
		return nil
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil || strings.TrimSpace(host) == "" {
		return errors.New("STUN server must be host:port or 'off'")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("STUN server port must be between 1 and 65535")
	}
	return nil
}

func (s *Session) gatherHostCandidates() {
	interfaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, parseErr := net.ParseCIDR(addr.String())
			if parseErr != nil || !usableIP(ip) {
				continue
			}
			network := "udp6"
			if ip.To4() != nil {
				network = "udp4"
			}
			ep := s.endpointForNetwork(network)
			if ep == nil {
				continue
			}
			port := ep.conn.LocalAddr().(*net.UDPAddr).Port
			s.addCandidate(Candidate{Network: network, Address: (&net.UDPAddr{IP: ip, Port: port}).String(), Kind: "host"}, MaxCandidates-2)
		}
	}
}

func (s *Session) gatherServerReflexiveCandidates(ctx context.Context, server string) {
	if strings.EqualFold(strings.TrimSpace(server), "off") {
		return
	}
	host, portText, err := net.SplitHostPort(server)
	if err != nil {
		return
	}
	port, err := net.LookupPort("udp", portText)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, ep := range s.endpoints {
		ep := ep
		wg.Add(1)
		go func() {
			defer wg.Done()
			ipNetwork := "ip6"
			if ep.network == "udp4" {
				ipNetwork = "ip4"
			}
			ips, lookupErr := net.DefaultResolver.LookupNetIP(ctx, ipNetwork, host)
			if lookupErr != nil || len(ips) == 0 {
				return
			}
			serverAddr := &net.UDPAddr{IP: net.IP(ips[0].AsSlice()), Port: port}
			mapped, queryErr := querySTUN(ctx, ep, serverAddr)
			if queryErr != nil || !usableIP(mapped.IP) {
				return
			}
			ep.stunAddr = serverAddr
			s.addCandidate(Candidate{Network: ep.network, Address: mapped.String(), Kind: "srflx"}, MaxCandidates)
		}()
	}
	wg.Wait()
}

func querySTUN(ctx context.Context, ep *endpoint, server *net.UDPAddr) (*net.UDPAddr, error) {
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := ep.transport.WriteTo(request.Raw, server); err != nil {
		return nil, err
	}
	buffer := make([]byte, 2048)
	for {
		n, from, err := ep.transport.ReadNonQUICPacket(ctx, buffer)
		if err != nil {
			return nil, err
		}
		fromUDP, ok := from.(*net.UDPAddr)
		if !ok || fromUDP.Port != server.Port || !fromUDP.IP.Equal(server.IP) {
			continue
		}
		response := new(stun.Message)
		response.Raw = append(response.Raw, buffer[:n]...)
		if err = response.Decode(); err != nil || response.Type != stun.BindingSuccess || response.TransactionID != request.TransactionID {
			continue
		}
		var mapped stun.XORMappedAddress
		if err = mapped.GetFrom(response); err != nil {
			return nil, err
		}
		return &net.UDPAddr{IP: mapped.IP, Port: mapped.Port}, nil
	}
}

func (s *Session) keepMappingsAlive() {
	ticker := time.NewTicker(defaultKeepAlive)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
			for _, ep := range s.endpointSnapshot() {
				if ep.stunAddr != nil {
					_, _ = ep.transport.WriteTo(request.Raw, ep.stunAddr)
				}
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Session) startProbing(ctx context.Context, candidates []Candidate) <-chan validatedRoute {
	routes := make(chan validatedRoute, 1)
	for _, ep := range s.endpointSnapshot() {
		ep := ep
		go func() {
			buffer := make([]byte, 2048)
			for {
				n, from, err := ep.transport.ReadNonQUICPacket(ctx, buffer)
				if err != nil {
					return
				}
				fromUDP, ok := from.(*net.UDPAddr)
				if !ok || !s.validProbe(buffer[:n]) {
					continue
				}
				select {
				case routes <- validatedRoute{endpoint: ep, addr: cloneUDPAddr(fromUDP)}:
				default:
				}
			}
		}()
	}
	go s.sendProbes(ctx, candidates)
	return routes
}

func (s *Session) sendProbes(ctx context.Context, candidates []Candidate) {
	if len(candidates) == 0 {
		return
	}
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	index := 0
	for {
		candidate := candidates[index%len(candidates)]
		index++
		ep := s.endpointForNetwork(candidate.Network)
		if ep != nil {
			if addr, err := parseCandidate(candidate); err == nil {
				_, _ = ep.transport.WriteTo(s.newProbe(), addr)
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Session) newProbe() []byte {
	packet := make([]byte, probeSize)
	copy(packet[:8], probeMagic[:])
	copy(packet[8:40], s.sessionID[:])
	packet[40] = byte(s.role)
	_, _ = rand.Read(packet[41:57])
	mac := hmac.New(sha256.New, s.probeKey[:])
	_, _ = mac.Write(packet[:57])
	copy(packet[57:], mac.Sum(nil)[:16])
	return packet
}

func (s *Session) validProbe(packet []byte) bool {
	if len(packet) != probeSize || !hmac.Equal(packet[:8], probeMagic[:]) || !hmac.Equal(packet[8:40], s.sessionID[:]) {
		return false
	}
	peerRole := RoleReceiver
	if s.role == RoleReceiver {
		peerRole = RoleSender
	}
	if Role(packet[40]) != peerRole {
		return false
	}
	mac := hmac.New(sha256.New, s.probeKey[:])
	_, _ = mac.Write(packet[:57])
	return hmac.Equal(packet[57:], mac.Sum(nil)[:16])
}

func validateOffer(offer Offer, sessionID [32]byte) (Offer, error) {
	if len(offer.SessionID) != len(sessionID) || !hmac.Equal(offer.SessionID, sessionID[:]) {
		return Offer{}, errors.New("direct QUIC offer has the wrong session ID")
	}
	if len(offer.CertSHA256) != sha256.Size {
		return Offer{}, fmt.Errorf("direct QUIC offer has invalid certificate fingerprint length %d", len(offer.CertSHA256))
	}
	if len(offer.Candidates) == 0 || len(offer.Candidates) > MaxCandidates {
		return Offer{}, fmt.Errorf("direct QUIC offer has invalid candidate count %d", len(offer.Candidates))
	}
	validated := Offer{
		SessionID:  append([]byte(nil), offer.SessionID...),
		CertSHA256: append([]byte(nil), offer.CertSHA256...),
	}
	seen := make(map[string]struct{}, len(offer.Candidates))
	for _, candidate := range offer.Candidates {
		addr, err := parseCandidate(candidate)
		if err != nil {
			return Offer{}, err
		}
		key := candidate.Network + "\x00" + addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidate.Address = addr.String()
		validated.Candidates = append(validated.Candidates, candidate)
	}
	if len(validated.Candidates) == 0 {
		return Offer{}, errors.New("direct QUIC offer contains no unique candidates")
	}
	return validated, nil
}

func parseCandidate(candidate Candidate) (*net.UDPAddr, error) {
	if candidate.Network != "udp4" && candidate.Network != "udp6" {
		return nil, fmt.Errorf("invalid direct QUIC candidate network %q", candidate.Network)
	}
	if candidate.Kind != "host" && candidate.Kind != "srflx" {
		return nil, fmt.Errorf("invalid direct QUIC candidate kind %q", candidate.Kind)
	}
	host, portText, err := net.SplitHostPort(candidate.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid direct QUIC candidate address: %w", err)
	}
	ip := net.ParseIP(host)
	if !usableIP(ip) {
		return nil, errors.New("direct QUIC candidate uses an unusable IP address")
	}
	port, err := net.LookupPort("udp", portText)
	if err != nil || port == 0 {
		return nil, errors.New("direct QUIC candidate uses an invalid port")
	}
	if candidate.Network == "udp4" && ip.To4() == nil {
		return nil, errors.New("direct QUIC udp4 candidate is not IPv4")
	}
	if candidate.Network == "udp6" && (ip.To4() != nil || ip.To16() == nil) {
		return nil, errors.New("direct QUIC udp6 candidate is not IPv6")
	}
	return &net.UDPAddr{IP: ip, Port: port}, nil
}

func usableIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast()
}

func (s *Session) addCandidate(candidate Candidate, limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.candidates {
		if existing.Network == candidate.Network && existing.Address == candidate.Address {
			return
		}
	}
	if len(s.candidates) < limit {
		s.candidates = append(s.candidates, candidate)
	}
}

func (s *Session) endpointForNetwork(network string) *endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ep := range s.endpoints {
		if ep.network == network {
			return ep
		}
	}
	return nil
}

func (s *Session) endpointSnapshot() []*endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*endpoint(nil), s.endpoints...)
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	clone := *addr
	clone.IP = append(net.IP(nil), addr.IP...)
	return &clone
}
