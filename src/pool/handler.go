package pool

import (
	"fmt"
	"net"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/schollz/croc/v10/src/utils"
	log "github.com/schollz/logger"
)

// RegisterRequest represents the payload for relay registration
type RegisterRequest struct {
	Address string `json:"address"`
	Version string `json:"version,omitempty"`
}

// RegisterResponse represents the response for relay registration.
type RegisterResponse struct {
	Status string `json:"status"`
	Token  string `json:"token"`
	State  string `json:"state"`
}

// HeartbeatRequest represents the payload for heartbeat updates
type HeartbeatRequest struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

type errorResponse struct {
	Error     string `json:"error"`
	Reason    string `json:"reason,omitempty"`
	SourceIP  string `json:"sourceIP,omitempty"`
	ClaimedIP string `json:"claimedIP,omitempty"`
}

func registerPoolRoutes(app *fiber.App, store *RelayStore) {
	app.Post("/register", handleRegister(store))
	app.Post("/heartbeat", handleHeartbeat(store))
	app.Get("/relays", handleGetRelays(store))
	app.Get("/health", handleHealth(store))
}

func validateRelayRequest(c fiber.Ctx, address string) (string, error) {
	if address == "" {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "Address is required", Reason: "missing_address"})
	}

	if isIPv6Address(address) {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "IPv6 relay addresses are not supported by this pool", Reason: "ipv6_not_supported"})
	}

	if err := utils.ValidatePublicRelayAddress(address); err != nil {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: err.Error(), Reason: "invalid_public_address"})
	}

	sourceIP, err := getSourceIP(c.IP())
	if err != nil {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "Could not determine source IP", Reason: "invalid_source_ip"})
	}

	claimedIP, err := claimedHostIP(address)
	if err != nil {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: err.Error(), Reason: "invalid_address_format"})
	}

	if !sourceMatchesClaimedHost(sourceIP, address) {
		return "", c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error:     "source IP does not match claimed relay host",
			Reason:    "source_claim_mismatch",
			SourceIP:  sourceIP,
			ClaimedIP: claimedIP,
		})
	}

	return sourceIP, nil
}

// HandleRegister processes POST /register requests
func handleRegister(store *RelayStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req RegisterRequest
		if err := c.Bind().JSON(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("Invalid JSON payload")
		}

		sourceIP, err := validateRelayRequest(c, req.Address)
		if err != nil {
			return err
		}

		token, _ := store.Register(req.Address, req.Version, sourceIP)
		log.Infof("Registered relay (pending validation): %s (version: %s)", req.Address, req.Version)

		return c.Status(fiber.StatusCreated).JSON(RegisterResponse{
			Status: "registered",
			Token:  token,
			State:  "pending",
		})
	}
}

// HandleHeartbeat processes POST /heartbeat requests
func handleHeartbeat(store *RelayStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req HeartbeatRequest
		if err := c.Bind().JSON(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).SendString("Invalid JSON payload")
		}

		if req.Token == "" {
			return c.Status(fiber.StatusBadRequest).JSON(errorResponse{Error: "Token is required", Reason: "missing_token"})
		}

		sourceIP, err := validateRelayRequest(c, req.Address)
		if err != nil {
			return err
		}

		found, active := store.Heartbeat(req.Address, req.Token, sourceIP)
		if !found {
			return c.Status(fiber.StatusUnauthorized).JSON(errorResponse{Error: "Unknown relay or invalid token", Reason: "unknown_relay_or_invalid_token"})
		}

		state := "pending"
		if active {
			state = "active"
		}
		return c.Status(fiber.StatusOK).JSON(map[string]string{
			"status": "ok",
			"state":  state,
		})
	}
}

// HandleGetRelays processes GET /relays requests
func handleGetRelays(store *RelayStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		relays := store.GetRelays()
		return c.Status(fiber.StatusOK).JSON(relays)
	}
}

func getSourceIP(remoteAddr string) (string, error) {
	if ip := net.ParseIP(strings.TrimSpace(remoteAddr)); ip != nil {
		return ip.String(), nil
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("remote host is not an IP: %s", host)
	}
	return host, nil
}

func sourceMatchesClaimedHost(sourceIP, relayAddress string) bool {
	claimedHost, _, err := net.SplitHostPort(strings.TrimSpace(relayAddress))
	if err != nil {
		return false
	}
	claimedHost = strings.Trim(claimedHost, "[]")
	src := net.ParseIP(sourceIP)
	claim := net.ParseIP(claimedHost)
	if src == nil || claim == nil {
		return false
	}
	return src.Equal(claim)
}

func claimedHostIP(relayAddress string) (string, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(relayAddress))
	if err != nil {
		return "", fmt.Errorf("invalid relay address format")
	}
	host = strings.Trim(host, "[]")
	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("relay host is not an IP")
	}
	return host, nil
}

// isIPv6Address checks if the relay address contains an IPv6 address
func isIPv6Address(relayAddress string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(relayAddress))
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	// If To4() returns nil, it's IPv6
	return ip.To4() == nil
}

// HandleHealth processes GET /health requests
func handleHealth(store *RelayStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		count := store.Count()
		return c.Status(fiber.StatusOK).JSON(map[string]interface{}{
			"status":     "ok",
			"relayCount": count,
		})
	}
}
