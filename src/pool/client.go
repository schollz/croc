package pool

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	fiberclient "github.com/gofiber/fiber/v3/client"
)

type relayListItem struct {
	Address       string  `json:"address"`
	Version       string  `json:"version"`
	UptimePercent float64 `json:"uptimePercent"`
	Ready         bool    `json:"ready"`
}

// FetchPoolRelays fetches the list of active relays from the relay pool API.
func FetchPoolRelays(poolURL string, timeout time.Duration) ([]RelayInfo, error) {
	if poolURL == "" {
		return nil, fmt.Errorf("pool API URL is empty")
	}

	client := fiberclient.New().SetTimeout(timeout)

	url := poolURL
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	url += "relays"

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch relays: %w", err)
	}
	defer resp.Close()

	if resp.StatusCode() != fiber.StatusOK {
		return nil, fmt.Errorf("pool API returned status %d: %s", resp.StatusCode(), string(resp.Body()))
	}

	var rawRelays []relayListItem
	if err := resp.JSON(&rawRelays); err != nil {
		return nil, fmt.Errorf("failed to decode relay list: %w", err)
	}

	// Convert to RelayInfo structs, filtering only ready relays
	var relays []RelayInfo
	for _, r := range rawRelays {
		// Only include ready relays in client pool
		if r.Ready && r.Address != "" {
			relays = append(relays, RelayInfo{
				Address:       r.Address,
				Version:       r.Version,
				UptimePercent: r.UptimePercent,
			})
		}
	}

	return relays, nil
}
