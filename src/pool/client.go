package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/schollz/croc/v10/src/models"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	cache      map[string]Relay
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = models.DEFAULT_POOL_URL
	}
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		cache: make(map[string]Relay),
	}
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody interface{}, respBody interface{}) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pool request failed: %s", resp.Status)
	}
	if respBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) Register(ctx context.Context, req RegisterRequest) (RegisterResponse, error) {
	var resp RegisterResponse
	err := c.postJSON(ctx, "/register", req, &resp)
	return resp, err
}

func (c *Client) Heartbeat(ctx context.Context, relayID string) error {
	return c.postJSON(ctx, "/heartbeat", HeartbeatRequest{RelayID: relayID}, nil)
}

func (c *Client) FetchRelays(ctx context.Context) ([]Relay, error) {
	var resp RelayListResponse
	err := c.postJSON(ctx, "/relays", map[string]string{}, &resp)
	if err != nil {
		return nil, err
	}
	for _, relay := range resp.Relays {
		c.cache[relay.RelayID] = relay
	}
	return resp.Relays, nil
}

func (c *Client) GetCachedRelay(relayID string) (Relay, bool) {
	relay, ok := c.cache[strings.ToLower(strings.TrimSpace(relayID))]
	return relay, ok
}

func (c *Client) ChooseRandomReachableRelay(relays []Relay) (Relay, bool) {
	reachable := make([]Relay, 0, len(relays))
	for _, relay := range relays {
		if isRelayReachable(relay, time.Second) {
			reachable = append(reachable, relay)
		}
	}
	if len(reachable) == 0 {
		return Relay{}, false
	}
	return reachable[rand.Intn(len(reachable))], true
}

func isRelayReachable(relay Relay, timeout time.Duration) bool {
	if len(relay.Ports) == 0 {
		return false
	}
	port := relay.Ports[0]
	if relay.IPv6 != "" {
		if canDial(net.JoinHostPort(relay.IPv6, port), timeout) {
			return true
		}
	}
	if relay.IPv4 != "" {
		if canDial(net.JoinHostPort(relay.IPv4, port), timeout) {
			return true
		}
	}
	return false
}

func canDial(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
