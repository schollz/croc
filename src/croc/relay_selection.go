package croc

import (
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/schollz/croc/v10/src/models"
	"github.com/schollz/croc/v10/src/pool"
	"github.com/schollz/croc/v10/src/utils"
	log "github.com/schollz/logger"
)

// relayScore represents a relay with its calculated score
type relayScore struct {
	address string
	latency time.Duration
	uptime  float64
	score   float64
}

// selectBestRelay chooses the best relay from a pool based on latency and uptime
func selectBestRelay(relays []pool.RelayInfo, sampleSize int) (string, error) {
	if len(relays) == 0 {
		return "", fmt.Errorf("no relays available")
	}

	// Sample random relays
	sample := sampleRelays(relays, sampleSize)
	if len(sample) == 0 {
		return "", fmt.Errorf("no relays to sample")
	}

	log.Debugf("Sampling %d relays from pool of %d", len(sample), len(relays))

	// Measure latency for each sampled relay in parallel
	var wg sync.WaitGroup
	scores := make([]relayScore, len(sample))

	for i, relay := range sample {
		wg.Add(1)
		go func(idx int, r pool.RelayInfo) {
			defer wg.Done()

			latency, err := utils.MeasureLatency(r.Address, 500*time.Millisecond)
			if err != nil {
				log.Debugf("Failed to measure latency for %s: %v", r.Address, err)
				scores[idx] = relayScore{
					address: r.Address,
					latency: 0,
					uptime:  r.GetUptimePercent(),
					score:   -1, // Mark as failed
				}
				return
			}

			// Calculate score: higher uptime and lower latency = higher score
			// score = (uptimePercent / 100) / latency_seconds
			latencySeconds := latency.Seconds()
			if latencySeconds == 0 {
				latencySeconds = 0.001 // Avoid division by zero
			}

			score := (r.GetUptimePercent() / 100.0) / latencySeconds

			log.Debugf("Relay %s: latency=%v, uptime=%.1f%%, score=%.2f",
				r.Address, latency, r.GetUptimePercent(), score)

			scores[idx] = relayScore{
				address: r.Address,
				latency: latency,
				uptime:  r.GetUptimePercent(),
				score:   score,
			}
		}(i, relay)
	}

	wg.Wait()

	// Filter out failed relays and sort by score
	validScores := make([]relayScore, 0, len(scores))
	for _, s := range scores {
		if s.score > 0 {
			validScores = append(validScores, s)
		}
	}

	if len(validScores) == 0 {
		return "", fmt.Errorf("all sampled relays are unreachable")
	}

	// Sort by score (highest first)
	sort.Slice(validScores, func(i, j int) bool {
		return validScores[i].score > validScores[j].score
	})

	bestRelay := validScores[0]
	log.Debugf("Selected best relay: %s (latency=%v, uptime=%.1f%%, score=%.2f)",
		bestRelay.address, bestRelay.latency, bestRelay.uptime, bestRelay.score)

	return bestRelay.address, nil
}

// sampleRelays randomly samples up to n relays from the pool
func sampleRelays(relays []pool.RelayInfo, n int) []pool.RelayInfo {
	if n >= len(relays) {
		return relays
	}

	// Create a copy and shuffle
	sampled := make([]pool.RelayInfo, len(relays))
	copy(sampled, relays)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(sampled), func(i, j int) {
		sampled[i], sampled[j] = sampled[j], sampled[i]
	})

	return sampled[:n]
}

func relayFallbackPort(c *Client) string {
	if len(c.Options.RelayPorts) > 0 {
		return c.Options.RelayPorts[0]
	}
	return models.DEFAULT_PORT
}

func splitRelayAddress(relayAddress, fallbackPort string) (host string, port string, err error) {
	relayAddress = strings.TrimSpace(relayAddress)
	if relayAddress == "" {
		return "", "", fmt.Errorf("empty relay address")
	}

	host, port, err = net.SplitHostPort(relayAddress)
	if err == nil {
		return strings.Trim(host, "[]"), port, nil
	}

	trimmed := strings.Trim(relayAddress, "[]")
	if ip := net.ParseIP(trimmed); ip != nil {
		return trimmed, fallbackPort, nil
	}
	if !strings.Contains(relayAddress, ":") {
		return relayAddress, fallbackPort, nil
	}

	return "", "", fmt.Errorf("invalid relay address %q", relayAddress)
}

// discoverRelay attempts to discover and select a relay using the pool API
func (c *Client) discoverRelay() (address string, port string, err error) {
	fallbackPort := relayFallbackPort(c)

	if c.Options.PoolURL == "" {
		return "", "", fmt.Errorf("no pool API URL")
	}

	log.Debugf("Fetching relay pool from pool API: %s", c.Options.PoolURL)

	// Fetch relay pool from pool API
	relays, err := pool.FetchPoolRelays(c.Options.PoolURL, 5*time.Second)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch relay pool: %w", err)
	}

	if len(relays) == 0 {
		return "", "", fmt.Errorf("pool API returned empty relay pool")
	}

	log.Debugf("Fetched %d relays from pool API", len(relays))

	// Select best relay based on latency and uptime
	bestRelay, err := selectBestRelay(relays, 5) // Sample 5 relays
	if err != nil {
		return "", "", fmt.Errorf("failed to select relay: %w", err)
	}

	host, relayPort, err := splitRelayAddress(bestRelay, fallbackPort)
	if err != nil {
		return "", "", err
	}
	address = host
	port = relayPort

	log.Debugf("Selected relay from pool: %s:%s", address, port)
	return address, port, nil
}
