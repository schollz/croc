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
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/schollz/croc/v11/src/publicrelay"
	"github.com/schollz/croc/v11/src/store"
	log "github.com/schollz/logger"
)

const (
	defaultDialTimeout  = 10 * time.Second
	maxWebSocketMessage = 65 * 1024 * 1024
)

// Config configures the unified croc web server.
type Config struct {
	ListenAddress string
	RelayHosts    []string
	// RelayHost is retained as a single-host configuration shorthand.
	RelayHost      string
	AllowedPorts   []string
	OriginPatterns []string
	DialTimeout    time.Duration
	PublicAddress  string
	RelayPassword  string
	StaticFiles    fs.FS
	StoreService   *store.Service
	UmamiURL       string
	UmamiWebsiteID string
	GoogleAdSense  string
	GoogleAdsTXT   string
}

type handler struct {
	relayHosts     []string
	allowedPorts   map[string]struct{}
	originPatterns []string
	dialTimeout    time.Duration
	runtimeConfig  runtimeConfig
	static         http.Handler
}

type runtimeConfig struct {
	GatewayURL     string             `json:"gatewayURL"`
	RelayAddresses []string           `json:"relayAddresses"`
	RelayPassword  string             `json:"relayPassword"`
	Store          store.PublicConfig `json:"store"`
}

// Handler returns the unified croc web handler. It serves the embedded client,
// /config.js, /healthz, and /ws?relay=<index>&port=<allowlisted relay port>.
func Handler(config Config) (http.Handler, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	static, err := newStaticHandler(
		normalized.StaticFiles,
		normalized.UmamiURL,
		normalized.UmamiWebsiteID,
		normalized.GoogleAdSense,
	)
	if err != nil {
		return nil, err
	}

	h := &handler{
		relayHosts:     normalized.RelayHosts,
		allowedPorts:   make(map[string]struct{}, len(normalized.AllowedPorts)),
		originPatterns: normalized.OriginPatterns,
		dialTimeout:    normalized.DialTimeout,
		runtimeConfig: runtimeConfig{
			GatewayURL: "/ws",
			RelayAddresses: func() []string {
				addresses := make([]string, len(normalized.RelayHosts))
				for index, host := range normalized.RelayHosts {
					addresses[index] = net.JoinHostPort(host, normalized.AllowedPorts[0])
				}
				return addresses
			}(),
			RelayPassword: normalized.RelayPassword,
			Store: func() store.PublicConfig {
				if normalized.StoreService == nil {
					return store.PublicConfig{}
				}
				return normalized.StoreService.PublicConfig()
			}(),
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
	if normalized.GoogleAdsTXT != "" {
		mux.HandleFunc("/ads.txt", adsTXT(normalized.GoogleAdsTXT))
	}
	if normalized.StoreService != nil {
		mux.Handle("/api/v1/store/transfers", normalized.StoreService)
		mux.Handle("/api/v1/store/transfers/", normalized.StoreService)
	}
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
	if normalized.StoreService != nil {
		go normalized.StoreService.RunCleanup(ctx)
	}
	go func() {
		log.Infof(
			"starting croc web server on %s for %s via %s (%s)",
			normalized.ListenAddress,
			normalized.PublicAddress,
			strings.Join(normalized.RelayHosts, ","),
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
	if len(config.RelayHosts) == 0 {
		if config.RelayHost != "" {
			config.RelayHosts = []string{config.RelayHost}
		} else {
			config.RelayHosts = publicrelay.Relays()
		}
	}
	if config.RelayPassword == "" {
		config.RelayPassword = "pass123"
	}
	hosts := make([]string, 0, len(config.RelayHosts))
	seenHosts := make(map[string]struct{}, len(config.RelayHosts))
	for _, rawHost := range config.RelayHosts {
		host := strings.TrimSpace(rawHost)
		if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#") {
			return Config{}, fmt.Errorf("relay must be a host, not a URL: %q", rawHost)
		}
		if splitHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = splitHost
		}
		host = strings.Trim(host, "[]")
		if host == "" {
			return Config{}, errors.New("relay host cannot be empty")
		}
		if _, exists := seenHosts[host]; exists {
			continue
		}
		seenHosts[host] = struct{}{}
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return Config{}, errors.New("relay host list cannot be empty")
	}
	config.RelayHosts = hosts
	config.RelayHost = hosts[0]

	if len(config.AllowedPorts) == 0 {
		config.AllowedPorts = []string{
			"9009",
			"9010", "9011", "9012", "9013",
			"9014", "9015", "9016", "9017",
		}
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

func adsTXT(contents string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(contents)))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, contents)
		}
	}
}

func (h *handler) websocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	relayIndex, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("relay")))
	if err != nil || relayIndex < 0 || relayIndex >= len(h.relayHosts) {
		http.Error(w, "relay index is not allowed", http.StatusForbidden)
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
		net.JoinHostPort(h.relayHosts[relayIndex], port),
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
	htmlPages  map[string][]byte
	installer  []byte
}

func newStaticHandler(
	files fs.FS,
	umamiURL string,
	umamiWebsiteID string,
	googleAdSense string,
) (http.Handler, error) {
	if files == nil {
		return nil, errors.New("static file system cannot be empty")
	}
	htmlPages := make(map[string][]byte)
	err := fs.WalkDir(files, ".", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || path.Base(filePath) != "index.html" {
			return nil
		}
		page, readErr := fs.ReadFile(files, filePath)
		if readErr != nil {
			return readErr
		}
		page = injectUmamiScript(page, umamiURL, umamiWebsiteID)
		page = injectGoogleAdSense(page, googleAdSense)
		htmlPages[filePath] = page
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded HTML pages: %w", err)
	}
	index, exists := htmlPages["index.html"]
	if !exists {
		return nil, fmt.Errorf("embedded web client is missing index.html")
	}
	installer, err := fs.ReadFile(files, "default.txt")
	if err != nil {
		return nil, fmt.Errorf("embedded web client is missing default.txt: %w", err)
	}
	return &staticHandler{
		files:      files,
		fileServer: http.FileServer(http.FS(files)),
		index:      index,
		htmlPages:  htmlPages,
		installer:  installer,
	}, nil
}

func injectGoogleAdSense(index []byte, publisherID string) []byte {
	publisherID = strings.TrimSpace(publisherID)
	if publisherID == "" {
		return index
	}

	const closingHead = "</head>"
	if !strings.Contains(string(index), closingHead) {
		return index
	}
	scriptURL := "https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=" +
		url.QueryEscape(publisherID)
	script := `<script async src="` + scriptURL + `" crossorigin="anonymous"></script>`
	return []byte(strings.Replace(string(index), closingHead, script+closingHead, 1))
}

func injectUmamiScript(index []byte, baseURL, websiteID string) []byte {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	websiteID = strings.TrimSpace(websiteID)
	if baseURL == "" || websiteID == "" {
		return index
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return index
	}
	scriptURL, err := url.JoinPath(baseURL, "script.js")
	if err != nil {
		return index
	}

	const closingHead = "</head>"
	if !strings.Contains(string(index), closingHead) {
		return index
	}
	script := `<script defer src="` + html.EscapeString(scriptURL) +
		`" data-website-id="` + html.EscapeString(websiteID) + `" data-performance="true"></script>`
	return []byte(strings.Replace(string(index), closingHead, script+closingHead, 1))
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path == "/" {
		w.Header().Set("Vary", "User-Agent")
	}
	if r.URL.Path == "/" && requestsInstaller(r.UserAgent()) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(h.installer)))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet {
			_, _ = w.Write(h.installer)
		}
		return
	}

	requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requested == "" || requested == "." {
		requested = "index.html"
	}
	info, err := fs.Stat(h.files, requested)
	if err == nil && info.IsDir() {
		routeIndex := path.Join(requested, "index.html")
		if _, exists := h.htmlPages[routeIndex]; exists {
			requested = routeIndex
		} else {
			requested = "index.html"
		}
	} else if err != nil {
		if path.Ext(requested) != "" {
			http.NotFound(w, r)
			return
		}
		routeIndex := path.Join(requested, "index.html")
		if _, exists := h.htmlPages[routeIndex]; exists {
			requested = routeIndex
		} else {
			requested = "index.html"
		}
	}

	if page, exists := h.htmlPages[requested]; exists {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(page)
		}
		return
	} else if requested == "croc-download-sw.js" || requested == "croc-worker.js" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.HasPrefix(requested, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	cloned := r.Clone(r.Context())
	cloned.URL.Path = "/" + requested
	h.fileServer.ServeHTTP(w, cloned)
}

func requestsInstaller(userAgent string) bool {
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	return strings.HasPrefix(userAgent, "curl/") ||
		strings.HasPrefix(userAgent, "wget/")
}
