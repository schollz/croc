// Package webrelay serves the embedded croc web client and exposes its
// deliberately small WebSocket-to-TCP bridge. The bridge never participates
// in the croc protocol; it only forwards an opaque byte stream to one fixed
// relay host and an allowlisted relay port.
package webrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/schollz/croc/v10/src/webassets"
	log "github.com/schollz/logger"
)

const (
	defaultDialTimeout  = 10 * time.Second
	maxWebSocketMessage = 65 * 1024 * 1024
)

// Config configures the unified croc web server.
type Config struct {
	ListenAddress  string
	RelayHost      string
	AllowedPorts   []string
	OriginPatterns []string
	DialTimeout    time.Duration
	PublicAddress  string
	RelayPassword  string
	StaticFiles    fs.FS
}

type handler struct {
	relayHost      string
	allowedPorts   map[string]struct{}
	originPatterns []string
	dialTimeout    time.Duration
	runtimeConfig  runtimeConfig
	static         http.Handler
}

type runtimeConfig struct {
	GatewayURL    string `json:"gatewayURL"`
	RelayAddress  string `json:"relayAddress"`
	RelayPassword string `json:"relayPassword"`
}

// Handler returns the unified croc web handler. It serves the embedded client,
// /config.js, /healthz, and /ws?port=<allowlisted relay port>.
func Handler(config Config) (http.Handler, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	static, err := newStaticHandler(normalized.StaticFiles)
	if err != nil {
		return nil, err
	}

	h := &handler{
		relayHost:      normalized.RelayHost,
		allowedPorts:   make(map[string]struct{}, len(normalized.AllowedPorts)),
		originPatterns: normalized.OriginPatterns,
		dialTimeout:    normalized.DialTimeout,
		runtimeConfig: runtimeConfig{
			GatewayURL:    "/ws",
			RelayAddress:  net.JoinHostPort(normalized.RelayHost, normalized.AllowedPorts[0]),
			RelayPassword: normalized.RelayPassword,
		},
		static: static,
	}
	for _, port := range normalized.AllowedPorts {
		h.allowedPorts[port] = struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.health)
	mux.HandleFunc("/ws", h.websocket)
	mux.HandleFunc("/config.js", h.config)
	mux.Handle("/", h.static)
	return mux, nil
}

// Run starts the unified web client and relay bridge and blocks until the
// context is cancelled or the server exits.
func Run(ctx context.Context, config Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeConfig(config)
	if err != nil {
		return err
	}
	httpHandler, err := Handler(normalized)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              normalized.ListenAddress,
		Handler:           httpHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Infof(
			"starting croc web server on %s for %s via %s (%s)",
			normalized.ListenAddress,
			normalized.PublicAddress,
			normalized.RelayHost,
			strings.Join(normalized.AllowedPorts, ","),
		)
		errc <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			return shutdownErr
		}
		err = <-errc
	case err = <-errc:
	}

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func normalizeConfig(config Config) (Config, error) {
	if config.ListenAddress == "" {
		config.ListenAddress = "127.0.0.1:9014"
	}
	if config.PublicAddress == "" {
		config.PublicAddress = config.ListenAddress
	}
	if config.RelayHost == "" {
		config.RelayHost = "croc.schollz.com"
	}
	if config.RelayPassword == "" {
		config.RelayPassword = "pass123"
	}
	if strings.Contains(config.RelayHost, "://") || strings.ContainsAny(config.RelayHost, "/?#") {
		return Config{}, fmt.Errorf("relay must be a host, not a URL: %q", config.RelayHost)
	}
	if host, _, splitErr := net.SplitHostPort(config.RelayHost); splitErr == nil {
		config.RelayHost = host
	}
	config.RelayHost = strings.Trim(config.RelayHost, "[]")
	if config.RelayHost == "" {
		return Config{}, errors.New("relay host cannot be empty")
	}

	if len(config.AllowedPorts) == 0 {
		config.AllowedPorts = []string{"9009", "9010", "9011", "9012", "9013"}
	}
	seen := make(map[string]struct{}, len(config.AllowedPorts))
	ports := make([]string, 0, len(config.AllowedPorts))
	for _, rawPort := range config.AllowedPorts {
		port := strings.TrimSpace(rawPort)
		portNumber, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || portNumber == 0 {
			return Config{}, fmt.Errorf("invalid relay port %q", rawPort)
		}
		port = strconv.FormatUint(portNumber, 10)
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	config.AllowedPorts = ports
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultDialTimeout
	}
	if config.StaticFiles == nil {
		config.StaticFiles = webassets.Files()
	}
	return config, nil
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (h *handler) config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, err := json.Marshal(h.runtimeConfig)
	if err != nil {
		http.Error(w, "could not create runtime configuration", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = fmt.Fprintf(w, "window.__CROC_RUNTIME_CONFIG__ = %s;\n", payload)
}

func (h *handler) websocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	port := strings.TrimSpace(r.URL.Query().Get("port"))
	if _, allowed := h.allowedPorts[port]; !allowed {
		http.Error(w, "relay port is not allowed", http.StatusForbidden)
		return
	}

	dialCtx, cancelDial := context.WithTimeout(r.Context(), h.dialTimeout)
	defer cancelDial()
	upstream, err := (&net.Dialer{}).DialContext(
		dialCtx,
		"tcp",
		net.JoinHostPort(h.relayHost, port),
	)
	if err != nil {
		http.Error(w, "relay is unavailable", http.StatusBadGateway)
		return
	}

	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns,
	})
	if err != nil {
		_ = upstream.Close()
		return
	}
	socket.SetReadLimit(maxWebSocketMessage)

	streamCtx, cancelStream := context.WithCancel(context.Background())
	web := websocket.NetConn(streamCtx, socket, websocket.MessageBinary)
	defer func() {
		cancelStream()
		_ = web.Close()
		_ = upstream.Close()
	}()

	errc := make(chan error, 2)
	var closeOnce sync.Once
	closeConnections := func() {
		closeOnce.Do(func() {
			cancelStream()
			_ = web.Close()
			_ = upstream.Close()
		})
	}
	copyStream := func(dst io.Writer, src io.Reader) {
		buffer := make([]byte, 64*1024)
		_, copyErr := io.CopyBuffer(dst, src, buffer)
		errc <- copyErr
		closeConnections()
	}

	go copyStream(upstream, web)
	go copyStream(web, upstream)
	<-errc
	<-errc
}

type staticHandler struct {
	files      fs.FS
	fileServer http.Handler
	index      []byte
}

func newStaticHandler(files fs.FS) (http.Handler, error) {
	if files == nil {
		return nil, errors.New("static file system cannot be empty")
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("embedded web client is missing index.html: %w", err)
	}
	return &staticHandler{
		files:      files,
		fileServer: http.FileServer(http.FS(files)),
		index:      index,
	}, nil
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requested == "" || requested == "." {
		requested = "index.html"
	}
	info, err := fs.Stat(h.files, requested)
	if err != nil || info.IsDir() {
		if path.Ext(requested) != "" {
			http.NotFound(w, r)
			return
		}
		requested = "index.html"
	}

	if requested == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(h.index)))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(h.index)
		}
		return
	} else if strings.HasPrefix(requested, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path = "/" + requested
	h.fileServer.ServeHTTP(w, cloned)
}
