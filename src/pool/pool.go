package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/schollz/croc/v10/src/models"
	log "github.com/schollz/logger"
)

type Server struct {
	Registry         *Registry
	HeartbeatTimeout time.Duration
	CleanupInterval  time.Duration
	ListenAddress    string
	httpServer       *http.Server
}

func NewServer(listenAddress string) *Server {
	if strings.TrimSpace(listenAddress) == "" {
		listenAddress = models.DEFAULT_POOL_LISTEN
	}
	return &Server{
		Registry:         NewRegistry(),
		HeartbeatTimeout: 30 * time.Second,
		CleanupInterval:  60 * time.Second,
		ListenAddress:    listenAddress,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/relays", s.handleRelays)

	s.httpServer = &http.Server{
		Addr:              s.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go s.runCleanupLoop(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()

	log.Infof("pool server listening on %s", s.ListenAddress)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) runCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(s.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := s.Registry.MarkInactiveOlderThan(s.HeartbeatTimeout)
			if changed > 0 {
				log.Infof("marked %d relays inactive", changed)
			}
		}
	}
}

func decodeJSON(r *http.Request, dst interface{}) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": msg})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}

	if len(req.Ports) < 2 {
		writeErr(w, http.StatusBadRequest, "ports must contain at least two entries")
		return
	}
	if strings.TrimSpace(req.IPv6) == "" && strings.TrimSpace(req.IPv4) == "" {
		writeErr(w, http.StatusBadRequest, "either ipv6 or ipv4 is required")
		return
	}
	if strings.TrimSpace(req.RelayID) != "" {
		log.Debugf("ignoring client-supplied relay_id=%s; main node assigns relay_id", req.RelayID)
	}

	ipForID := strings.TrimSpace(req.IPv6)
	if ipForID == "" {
		ipForID = strings.TrimSpace(req.IPv4)
	}
	relayID, err := GenerateRelayIDFromIP(ipForID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	relay := Relay{
		RelayID:  relayID,
		IPv6:     strings.TrimSpace(req.IPv6),
		IPv4:     strings.TrimSpace(req.IPv4),
		Ports:    req.Ports,
		Password: req.Password,
	}
	s.Registry.Upsert(relay)
	log.Infof("registered relay %s (ipv6=%s ipv4=%s)", relayID, relay.IPv6, relay.IPv4)
	writeJSON(w, http.StatusOK, RegisterResponse{OK: true, RelayID: relayID})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req HeartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	id := strings.ToLower(strings.TrimSpace(req.RelayID))
	if !IsRelayID(id) {
		writeErr(w, http.StatusBadRequest, "invalid relay_id")
		return
	}
	ok := s.Registry.Heartbeat(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "relay_id not found")
		return
	}
	log.Debugf("heartbeat relay %s", id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleRelays(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	relays := s.Registry.ListActive(50)
	writeJSON(w, http.StatusOK, RelayListResponse{Relays: relays})
}
