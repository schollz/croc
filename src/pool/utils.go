package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var relayIDRegex = regexp.MustCompile(`^[a-f0-9]{4}$`)

var privateIPv4Blocks = mustParseCIDRs([]string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	blocks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("pool: invalid CIDR %q: %v", cidr, err))
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// GenerateRelayIDFromIP returns a deterministic 4-char relay ID from an IP string.
func GenerateRelayIDFromIP(ip string) (string, error) {
	trimmed := strings.TrimSpace(ip)
	if trimmed == "" {
		return "", errors.New("ip is required")
	}
	parsed := net.ParseIP(trimmed)
	if parsed == nil {
		return "", fmt.Errorf("invalid ip: %s", ip)
	}
	sum := sha256.Sum256([]byte(parsed.String()))
	return hex.EncodeToString(sum[:2]), nil
}

func IsRelayID(s string) bool {
	return relayIDRegex.MatchString(strings.ToLower(strings.TrimSpace(s)))
}

func SanitizePorts(ports []string) ([]string, error) {
	if len(ports) < 2 {
		return nil, errors.New("ports must contain at least two entries")
	}
	sanitized := make([]string, 0, len(ports))
	for _, p := range ports {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return nil, errors.New("ports entries must be non-empty")
		}
		port, err := strconv.Atoi(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: must be numeric", p)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port %q: must be in range 1-65535", p)
		}
		sanitized = append(sanitized, trimmed)
	}
	return sanitized, nil
}

// ParseTransferCode splits a code into relayID + secret for federated format.
// Legacy codes return empty relayID and the original code as secret.
func ParseTransferCode(code string) (relayID string, secret string) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", ""
	}
	parts := strings.Split(code, "-")
	if len(parts) < 3 {
		return "", code
	}
	candidate := strings.ToLower(parts[0])
	if !IsRelayID(candidate) {
		return "", code
	}
	// The second segment must be the 4-digit pin in federated format.
	if len(parts[1]) != 4 {
		return "", code
	}
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return "", code
		}
	}
	secretPart := strings.Join(parts[1:], "-")
	if strings.TrimSpace(secretPart) == "" {
		return "", code
	}
	return candidate, secretPart
}

func IsPublicIPv4(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	ip = ip.To4()
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}

	// RFC1918 + CGNAT
	for _, block := range privateIPv4Blocks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func IsPublicIPv6(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil || ip.To4() != nil {
		return false
	}
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	// Unique local addresses fc00::/7 are not public.
	_, ula, _ := net.ParseCIDR("fc00::/7")
	if ula.Contains(ip) {
		return false
	}
	return true
}
