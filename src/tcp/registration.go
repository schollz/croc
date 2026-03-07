package tcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/schollz/logger"
)

// registrationConfig holds configuration for relay registration
type registrationConfig struct {
	poolURL           string
	relayAddresses    []string // Support multiple addresses (IPv4 only; IPv6 not supported by pool API)
	version           string
	heartbeatInterval time.Duration
}

// registerRequest represents the JSON payload for registration
type registerRequest struct {
	Address string `json:"address"`
	Version string `json:"version,omitempty"`
}

// heartbeatRequest represents the JSON payload for heartbeat
type heartbeatRequest struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

type registerResponse struct {
	Status string `json:"status"`
	Token  string `json:"token"`
	State  string `json:"state"`
}

// startRegistrationLoop registers the relay with the pool API and sends periodic heartbeats
func startRegistrationLoop(ctx context.Context, config registrationConfig) {
	if config.poolURL == "" || len(config.relayAddresses) == 0 {
		log.Debug("Registration disabled: pool API URL or relay addresses not set")
		return
	}

	log.Debugf("Starting registration loop for %d relay address(es) with pool API %s",
		len(config.relayAddresses), config.poolURL)

	// Track tokens per address
	tokens := make(map[string]string)

	// Initial registration for all addresses
	for _, addr := range config.relayAddresses {
		token, err := registerRelay(config.poolURL, addr, config.version)
		if err != nil {
			log.Errorf("Failed to register relay %s: %v", addr, err)
			log.Infof("Will retry %s on next heartbeat", addr)
		} else {
			tokens[addr] = token
			log.Infof("Successfully registered relay %s with pool API", addr)
		}
	}

	// Start heartbeat loop
	ticker := time.NewTicker(config.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug("Registration loop stopping due to context cancellation")
			return
		case <-ticker.C:
			for _, addr := range config.relayAddresses {
				token, hasToken := tokens[addr]
				if !hasToken || token == "" {
					token, err := registerRelay(config.poolURL, addr, config.version)
					if err != nil {
						log.Errorf("Failed to re-register relay %s: %v", addr, err)
						continue
					}
					tokens[addr] = token
					continue
				}

				if err := sendHeartbeat(config.poolURL, addr, token); err != nil {
					log.Errorf("Failed to send heartbeat for %s: %v", addr, err)
					// Pool API may have restarted; re-register next tick
					delete(tokens, addr)
				} else {
					log.Debugf("Sent heartbeat for relay %s", addr)
				}
			}
		}
	}
}

// registerRelay performs the initial registration with the pool API
func registerRelay(poolURL, address, version string) (string, error) {
	url := poolURL
	if url[len(url)-1] != '/' {
		url += "/"
	}
	url += "register"

	payload := registerRequest{
		Address: address,
		Version: version,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal registration data: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to send registration request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("pool API returned status %d: %s", resp.StatusCode, string(body))
	}

	var regResp registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return "", fmt.Errorf("failed to decode registration response: %w", err)
	}
	if regResp.Token == "" {
		return "", fmt.Errorf("pool API did not provide heartbeat token")
	}

	return regResp.Token, nil
}

// sendHeartbeat sends a heartbeat to refresh the relay's TTL
func sendHeartbeat(poolURL, address, token string) error {
	url := poolURL
	if url[len(url)-1] != '/' {
		url += "/"
	}
	url += "heartbeat"

	payload := heartbeatRequest{
		Address: address,
		Token:   token,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat data: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send heartbeat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pool API returned status %d", resp.StatusCode)
	}

	return nil
}
