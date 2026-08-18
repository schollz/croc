package publicrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/schollz/logger"
)

const (
	umamiQueueSize      = 64
	umamiRequestTimeout = 2 * time.Second
)

// UmamiReporter sends events to Umami without blocking its callers.
type UmamiReporter struct {
	endpoint  string
	websiteID string
	version   string
	client    *http.Client
	events    chan string
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type umamiRequest struct {
	Payload umamiPayload `json:"payload"`
	Type    string       `json:"type"`
}

type umamiPayload struct {
	Hostname string            `json:"hostname"`
	URL      string            `json:"url"`
	Website  string            `json:"website"`
	Name     string            `json:"name"`
	Data     map[string]string `json:"data"`
}

// NewUmamiReporter constructs and starts an Umami event reporter.
func NewUmamiReporter(baseURL, websiteID, version string) (*UmamiReporter, error) {
	baseURL = strings.TrimSpace(baseURL)
	websiteID = strings.TrimSpace(websiteID)
	if baseURL == "" {
		return nil, fmt.Errorf("Umami URL cannot be empty")
	}
	if websiteID == "" {
		return nil, fmt.Errorf("Umami website ID cannot be empty")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("Umami URL must be an absolute HTTP or HTTPS URL")
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	endpoint, err := url.JoinPath(parsedURL.String(), "api/send")
	if err != nil {
		return nil, fmt.Errorf("construct Umami endpoint: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reporter := &UmamiReporter{
		endpoint:  endpoint,
		websiteID: websiteID,
		version:   version,
		client:    &http.Client{Timeout: umamiRequestTimeout},
		events:    make(chan string, umamiQueueSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	reporter.wg.Add(1)
	go reporter.run()
	return reporter, nil
}

// Track queues an event if reporting is active and capacity is available.
func (r *UmamiReporter) Track(name string) {
	if r == nil || strings.TrimSpace(name) == "" {
		return
	}
	select {
	case <-r.ctx.Done():
		return
	default:
	}
	select {
	case r.events <- name:
	case <-r.ctx.Done():
	default:
		log.Debug("dropping Umami event because the analytics queue is full")
	}
}

// Close cancels in-flight reporting and drops any queued events.
func (r *UmamiReporter) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.cancel()
		r.wg.Wait()
	})
}

func (r *UmamiReporter) run() {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case name := <-r.events:
			r.send(name)
		}
	}
}

func (r *UmamiReporter) send(name string) {
	payload := umamiRequest{
		Payload: umamiPayload{
			Hostname: "relay",
			URL:      "/relay",
			Website:  r.websiteID,
			Name:     name,
			Data: map[string]string{
				"version": r.version,
			},
		},
		Type: "event",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Debugf("could not encode Umami event: %v", err)
		return
	}

	request, err := http.NewRequestWithContext(r.ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		log.Debugf("could not create Umami request: %v", err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "croc-relay/"+r.version)

	response, err := r.client.Do(request)
	if err != nil {
		if r.ctx.Err() == nil {
			log.Debugf("could not send Umami event: %v", err)
		}
		return
	}
	response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		log.Debugf("Umami event returned HTTP status %s", response.Status)
	}
}
