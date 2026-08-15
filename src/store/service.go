// Package store implements croc's anonymous, encrypted, download-limited
// temporary storage service. It stores only ciphertext and capability verifiers.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/diskusage"
	"github.com/schollz/croc/v11/src/storecrypto"
)

const (
	DefaultMaxTransfer   = int64(1 << 30)
	DefaultMaxTotal      = int64(5 << 30)
	DefaultMinFree       = int64(512 << 20)
	DefaultMaxFiles      = 100
	DefaultMaxDownloads  = 1
	DefaultCreatePerHour = 5
	DefaultActiveUploads = 2
	MaxManifestBytes     = int64(256 << 10)
	MaxChunkObjects      = 512
	transferLockStripes  = 256

	uploadLifetime    = time.Hour
	claimLifetime     = 30 * time.Minute
	tombstoneLifetime = 24 * time.Hour
	cleanupInterval   = time.Minute
)

type state string

const (
	stateUploading state = "uploading"
	stateAvailable state = "available"
	stateClaimed   state = "claimed"
	stateConsumed  state = "consumed"
	stateRevoked   state = "revoked"
	stateExpired   state = "expired"
)

// Config configures a filesystem-backed stored-transfer service.
type Config struct {
	Root             string
	MaxTransferBytes int64
	MaxTotalBytes    int64
	MinFreeBytes     int64
	MaxFiles         int
	MaxDownloads     int
	MaxExpiration    time.Duration
	CreatePerHour    int
	MaxActiveUploads int
	TrustedProxies   []netip.Prefix
	Now              func() time.Time
	CleanupInterval  time.Duration
	DisableRootLock  bool
}

// PublicConfig is safe to expose to web clients.
type PublicConfig struct {
	Enabled           bool  `json:"enabled"`
	MaxTransferBytes  int64 `json:"maxTransferBytes"`
	MaxFiles          int   `json:"maxFiles"`
	MaxDownloads      int   `json:"maxDownloads"`
	ExpiresSeconds    int64 `json:"expiresSeconds"`
	MaxExpiresSeconds int64 `json:"maxExpiresSeconds"`
}

type metadata struct {
	Version            int       `json:"version"`
	ID                 string    `json:"id"`
	State              state     `json:"state"`
	CreatedAt          time.Time `json:"createdAt"`
	UploadExpiresAt    time.Time `json:"uploadExpiresAt,omitempty"`
	CompletedAt        time.Time `json:"completedAt,omitempty"`
	ExpiresAt          time.Time `json:"expiresAt,omitempty"`
	ExpiresSeconds     int64     `json:"expiresSeconds,omitempty"`
	TombstoneExpiresAt time.Time `json:"tombstoneExpiresAt,omitempty"`
	ManifestBytes      int64     `json:"manifestBytes"`
	ChunkBytes         []int64   `json:"chunkBytes"`
	ReservedBytes      int64     `json:"reservedBytes"`
	UploadVerifier     string    `json:"uploadVerifier"`
	RedeemVerifier     string    `json:"redeemVerifier"`
	ClaimVerifier      string    `json:"claimVerifier,omitempty"`
	ClaimExpiresAt     time.Time `json:"claimExpiresAt,omitempty"`
	DownloadsTotal     int       `json:"downloadsTotal,omitempty"`
	DownloadsRemaining int       `json:"downloadsRemaining,omitempty"`
	CommittedClaims    []string  `json:"committedClaims,omitempty"`
	ClientIP           string    `json:"clientIP,omitempty"`
}

type creationWindow struct {
	times []time.Time
}

// Service owns a filesystem store and serves its versioned HTTP API.
type Service struct {
	config Config
	now    func() time.Time

	mu               sync.Mutex
	transferLockSeed maphash.Seed
	transferLocks    [transferLockStripes]sync.Mutex
	reservedBytes    int64
	activeUploads    map[string]int
	creationWindows  map[string]*creationWindow
	rootLock         *rootLock
}

type createRequest struct {
	Protocol       string  `json:"protocol"`
	ManifestBytes  int64   `json:"manifestBytes"`
	ChunkBytes     []int64 `json:"chunkBytes"`
	RedeemVerifier string  `json:"redeemVerifier"`
	DeclaredFiles  int     `json:"files"`
	PlaintextBytes int64   `json:"plaintextBytes"`
	Downloads      *int    `json:"downloads,omitempty"`
	ExpiresSeconds *int64  `json:"expiresSeconds,omitempty"`
}

type createResponse struct {
	ID              string    `json:"id"`
	UploadToken     string    `json:"uploadToken"`
	UploadExpiresAt time.Time `json:"uploadExpiresAt"`
	ChunkSize       int       `json:"chunkSize"`
}

type claimResponse struct {
	ClaimToken     string    `json:"claimToken"`
	ClaimExpiresAt time.Time `json:"claimExpiresAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type completeResponse struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

// New creates or opens a filesystem store and reconstructs quota state.
func New(config Config) (*Service, error) {
	if strings.TrimSpace(config.Root) == "" {
		return nil, errors.New("stored-transfer directory cannot be empty")
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve stored-transfer directory: %w", err)
	}
	config.Root = root
	if config.MaxTransferBytes <= 0 {
		config.MaxTransferBytes = DefaultMaxTransfer
	}
	if config.MaxTransferBytes > int64(MaxChunkObjects)*storecrypto.ChunkSize {
		return nil, fmt.Errorf(
			"stored-transfer byte limit cannot exceed %d bytes",
			int64(MaxChunkObjects)*storecrypto.ChunkSize,
		)
	}
	if config.MaxTotalBytes <= 0 {
		config.MaxTotalBytes = DefaultMaxTotal
	}
	if config.MinFreeBytes < 0 {
		return nil, errors.New("stored-transfer free-space reserve cannot be negative")
	}
	if config.MinFreeBytes == 0 {
		config.MinFreeBytes = DefaultMinFree
	}
	if config.MaxFiles <= 0 {
		config.MaxFiles = DefaultMaxFiles
	}
	if config.MaxFiles > storecrypto.MaxFiles {
		return nil, fmt.Errorf("stored-transfer file limit cannot exceed %d", storecrypto.MaxFiles)
	}
	if config.MaxDownloads <= 0 {
		config.MaxDownloads = DefaultMaxDownloads
	}
	if config.MaxExpiration < 0 {
		return nil, errors.New("stored-transfer maximum expiration cannot be negative")
	}
	if config.MaxExpiration > 0 && config.MaxExpiration < MinExpiration {
		return nil, fmt.Errorf("stored-transfer maximum expiration must be at least %s", MinExpiration)
	}
	if config.CreatePerHour <= 0 {
		config.CreatePerHour = DefaultCreatePerHour
	}
	if config.MaxActiveUploads <= 0 {
		config.MaxActiveUploads = DefaultActiveUploads
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = cleanupInterval
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create stored-transfer directory: %w", err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure stored-transfer directory: %w", err)
	}

	var lock *rootLock
	if !config.DisableRootLock {
		lock, err = acquireRootLock(root)
		if err != nil {
			return nil, err
		}
	}

	service := &Service{
		config:           config,
		now:              config.Now,
		transferLockSeed: maphash.MakeSeed(),
		activeUploads:    make(map[string]int),
		creationWindows:  make(map[string]*creationWindow),
		rootLock:         lock,
	}
	if err = service.recover(); err != nil {
		if lock != nil {
			_ = lock.Close()
		}
		return nil, err
	}
	if err = service.Sweep(); err != nil {
		if lock != nil {
			_ = lock.Close()
		}
		return nil, err
	}
	return service, nil
}

// Close releases the exclusive store-root lock.
func (s *Service) Close() error {
	if s.rootLock == nil {
		return nil
	}
	return s.rootLock.Close()
}

// RunCleanup sweeps expired data until the context is cancelled.
func (s *Service) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Sweep()
		}
	}
}

// PublicConfig returns browser-safe limits.
func (s *Service) PublicConfig() PublicConfig {
	effectiveDefault := DefaultExpiration
	if s.config.MaxExpiration > 0 && s.config.MaxExpiration < effectiveDefault {
		effectiveDefault = s.config.MaxExpiration
	}
	return PublicConfig{
		Enabled:           true,
		MaxTransferBytes:  s.config.MaxTransferBytes,
		MaxFiles:          s.config.MaxFiles,
		MaxDownloads:      s.config.MaxDownloads,
		ExpiresSeconds:    int64(effectiveDefault / time.Second),
		MaxExpiresSeconds: int64(s.config.MaxExpiration / time.Second),
	}
}

func (s *Service) transferDir(id string) string {
	return filepath.Join(s.config.Root, id[:2], id)
}

func (s *Service) metadataPath(id string) string {
	return filepath.Join(s.transferDir(id), "metadata.json")
}

func (s *Service) manifestPath(id string) string {
	return filepath.Join(s.transferDir(id), "manifest.bin")
}

func (s *Service) chunkPath(id string, index int) string {
	return filepath.Join(s.transferDir(id), "chunks", fmt.Sprintf("%08d.bin", index))
}

func validID(id string) bool {
	decoded, err := storecrypto.DecodeBase64URL(id)
	return err == nil && len(decoded) == storecrypto.TransferIDLen &&
		storecrypto.EncodeBase64URL(decoded) == id
}

func (s *Service) lockFor(id string) *sync.Mutex {
	// ponytail: fixed striping bounds attacker-controlled lock state; increase
	// the stripe count only if profiling shows meaningful contention.
	return &s.transferLocks[maphash.String(s.transferLockSeed, id)%transferLockStripes]
}

func (s *Service) recover() error {
	return filepath.WalkDir(s.config.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "metadata.json" {
			return nil
		}
		meta, err := readMetadata(path)
		if err != nil {
			return fmt.Errorf("read stored-transfer metadata %s: %w", path, err)
		}
		if !validID(meta.ID) || s.metadataPath(meta.ID) != path {
			return fmt.Errorf("stored-transfer metadata has invalid id %q", meta.ID)
		}
		s.reservedBytes += meta.ReservedBytes
		if meta.State == stateUploading && meta.ClientIP != "" {
			s.activeUploads[meta.ClientIP]++
		}
		return nil
	})
}

// Sweep expires incomplete and available transfers and removes old tombstones.
func (s *Service) Sweep() error {
	now := s.now()
	var ids []string
	err := filepath.WalkDir(s.config.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "metadata.json" {
			meta, readErr := readMetadata(path)
			if readErr != nil {
				return readErr
			}
			ids = append(ids, meta.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		lock := s.lockFor(id)
		lock.Lock()
		meta, readErr := s.load(id)
		if readErr != nil {
			lock.Unlock()
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return readErr
		}
		switch {
		case meta.State == stateUploading && !meta.UploadExpiresAt.After(now):
			readErr = s.tombstone(meta, stateExpired)
		case (meta.State == stateAvailable || meta.State == stateClaimed) && !meta.ExpiresAt.After(now):
			readErr = s.tombstone(meta, stateExpired)
		case meta.State == stateClaimed && !meta.ClaimExpiresAt.After(now):
			meta.State = stateAvailable
			meta.ClaimVerifier = ""
			meta.ClaimExpiresAt = time.Time{}
			readErr = s.save(meta)
		case isTombstone(meta.State):
			readErr = s.purgeCiphertext(meta.ID)
			if readErr == nil && !meta.TombstoneExpiresAt.After(now) {
				readErr = s.removeTransfer(meta)
			}
		}
		lock.Unlock()
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func isTombstone(value state) bool {
	return value == stateConsumed || value == stateRevoked || value == stateExpired
}

func (s *Service) load(id string) (*metadata, error) {
	if !validID(id) {
		return nil, os.ErrNotExist
	}
	return readMetadata(s.metadataPath(id))
}

func readMetadata(path string) (*metadata, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta metadata
	if err = json.Unmarshal(bytes, &meta); err != nil {
		return nil, err
	}
	// Metadata written before configurable downloads implicitly represented one
	// download. Preserve that behavior when an existing store is reopened.
	if meta.DownloadsTotal == 0 {
		meta.DownloadsTotal = 1
		if !isTombstone(meta.State) {
			meta.DownloadsRemaining = 1
		}
	}
	return &meta, nil
}

func (s *Service) save(meta *metadata) error {
	return writeJSONAtomic(s.metadataPath(meta.ID), meta)
}

func writeJSONAtomic(destination string, value any) error {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(destination, int64(len(bytes)), "", strings.NewReader(string(bytes)))
}

func writeAtomic(destination string, expected int64, expectedDigest string, input io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".upload-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	_ = temp.Chmod(0o600)

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(input, expected+1))
	if copyErr != nil {
		_ = temp.Close()
		return copyErr
	}
	if written != expected {
		_ = temp.Close()
		return fmt.Errorf("expected %d bytes, received %d", expected, written)
	}
	if expectedDigest != "" && storecrypto.EncodeBase64URL(hash.Sum(nil)) != expectedDigest {
		_ = temp.Close()
		return errors.New("ciphertext digest did not match")
	}
	if err = temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func (s *Service) tombstone(meta *metadata, terminal state) error {
	if isTombstone(meta.State) {
		return s.purgeCiphertext(meta.ID)
	}
	wasUploading := meta.State == stateUploading
	meta.State = terminal
	meta.ClaimExpiresAt = time.Time{}
	meta.TombstoneExpiresAt = s.now().Add(tombstoneLifetime)
	meta.ClaimVerifier = ""
	reservation := meta.ReservedBytes
	meta.ReservedBytes = 0
	if err := s.save(meta); err != nil {
		meta.ReservedBytes = reservation
		return err
	}
	purgeErr := s.purgeCiphertext(meta.ID)
	s.mu.Lock()
	s.reservedBytes -= reservation
	if s.reservedBytes < 0 {
		s.reservedBytes = 0
	}
	s.mu.Unlock()
	if wasUploading && meta.ClientIP != "" {
		s.mu.Lock()
		if s.activeUploads[meta.ClientIP] > 0 {
			s.activeUploads[meta.ClientIP]--
		}
		s.mu.Unlock()
	}
	return purgeErr
}

func (s *Service) purgeCiphertext(id string) error {
	manifestErr := os.Remove(s.manifestPath(id))
	if errors.Is(manifestErr, os.ErrNotExist) {
		manifestErr = nil
	}
	chunksErr := os.RemoveAll(filepath.Join(s.transferDir(id), "chunks"))
	return errors.Join(manifestErr, chunksErr)
}

func (s *Service) removeTransfer(meta *metadata) error {
	if err := os.RemoveAll(s.transferDir(meta.ID)); err != nil {
		return err
	}
	s.mu.Lock()
	s.reservedBytes -= meta.ReservedBytes
	if s.reservedBytes < 0 {
		s.reservedBytes = 0
	}
	s.mu.Unlock()
	return nil
}

func randomCapability() ([]byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}

func verifier(value []byte) string {
	return storecrypto.EncodeBase64URL(storecrypto.CapabilityVerifier(value))
}

func capabilityMatches(encoded string, verifierValue string) bool {
	value, err := storecrypto.DecodeBase64URL(encoded)
	if err != nil || len(value) != 32 {
		return false
	}
	actual := storecrypto.CapabilityVerifier(value)
	expected, err := storecrypto.DecodeBase64URL(verifierValue)
	return err == nil && len(expected) == len(actual) &&
		subtle.ConstantTimeCompare(actual, expected) == 1
}

func bearer(request *http.Request) string {
	scheme, value, ok := strings.Cut(strings.TrimSpace(request.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *Service) clientIP(request *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		remoteHost = request.RemoteAddr
	}
	remote, err := netip.ParseAddr(strings.Trim(remoteHost, "[]"))
	if err == nil {
		for _, prefix := range s.config.TrustedProxies {
			if prefix.Contains(remote) {
				forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0])
				if address, parseErr := netip.ParseAddr(forwarded); parseErr == nil {
					return address.String()
				}
				break
			}
		}
		return remote.String()
	}
	return remoteHost
}

func (s *Service) allowCreation(ip string) bool {
	now := s.now()
	cutoff := now.Add(-time.Hour)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeUploads[ip] >= s.config.MaxActiveUploads {
		return false
	}
	window := s.creationWindows[ip]
	if window == nil {
		window = new(creationWindow)
		s.creationWindows[ip] = window
	}
	recent := window.times[:0]
	for _, created := range window.times {
		if created.After(cutoff) {
			recent = append(recent, created)
		}
	}
	window.times = recent
	if len(recent) >= s.config.CreatePerHour {
		return false
	}
	window.times = append(window.times, now)
	s.activeUploads[ip]++
	return true
}

func (s *Service) reserve(bytes int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bytes <= 0 || bytes > s.config.MaxTotalBytes-s.reservedBytes {
		return false
	}
	usage := diskusage.NewDiskUsage(s.config.Root)
	if usage == nil || int64(usage.Available()) < bytes+s.config.MinFreeBytes {
		return false
	}
	s.reservedBytes += bytes
	return true
}

func (s *Service) unreserve(bytes int64) {
	s.mu.Lock()
	s.reservedBytes -= bytes
	if s.reservedBytes < 0 {
		s.reservedBytes = 0
	}
	s.mu.Unlock()
}

// ServeHTTP serves /api/v1/store/transfers and its descendants.
func (s *Service) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if !sameOriginRequest(request) {
		http.Error(response, "cross-origin stored-transfer requests are not allowed", http.StatusForbidden)
		return
	}

	relative := strings.TrimPrefix(request.URL.Path, "/api/v1/store/transfers")
	segments := strings.Split(strings.Trim(relative, "/"), "/")
	if relative == "" || relative == "/" {
		if request.Method == http.MethodPost {
			s.create(response, request)
			return
		}
		methodNotAllowed(response)
		return
	}
	if len(segments) < 1 || !validID(segments[0]) {
		http.NotFound(response, request)
		return
	}
	id := segments[0]
	switch {
	case len(segments) == 2 && segments[1] == "manifest" && request.Method == http.MethodPut:
		s.uploadObject(response, request, id, -1)
	case len(segments) == 3 && segments[1] == "chunks" && request.Method == http.MethodPut:
		index, err := strconv.Atoi(segments[2])
		if err != nil {
			http.NotFound(response, request)
			return
		}
		s.uploadObject(response, request, id, index)
	case len(segments) == 2 && segments[1] == "complete" && request.Method == http.MethodPost:
		s.complete(response, request, id)
	case len(segments) == 2 && segments[1] == "manifest" && request.Method == http.MethodGet:
		s.downloadManifest(response, request, id)
	case len(segments) == 2 && segments[1] == "claim" && request.Method == http.MethodPost:
		s.claim(response, request, id)
	case len(segments) == 2 && segments[1] == "claim" && request.Method == http.MethodDelete:
		s.release(response, request, id)
	case len(segments) == 3 && segments[1] == "chunks" && request.Method == http.MethodGet:
		index, err := strconv.Atoi(segments[2])
		if err != nil {
			http.NotFound(response, request)
			return
		}
		s.downloadChunk(response, request, id, index)
	case len(segments) == 2 && segments[1] == "commit" && request.Method == http.MethodPost:
		s.commit(response, request, id)
	case len(segments) == 1 && request.Method == http.MethodDelete:
		s.revoke(response, request, id)
	default:
		methodNotAllowed(response)
	}
}

func sameOriginRequest(request *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.User == nil &&
		strings.EqualFold(parsed.Host, request.Host) &&
		parsed.Path == "" &&
		parsed.RawQuery == "" &&
		parsed.Fragment == ""
}

func methodNotAllowed(response http.ResponseWriter) {
	http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func decodeJSON(request *http.Request, value any, max int64) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, max))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("request has trailing JSON")
	}
	return nil
}

func (s *Service) create(response http.ResponseWriter, request *http.Request) {
	ip := s.clientIP(request)
	if !s.allowCreation(ip) {
		response.Header().Set("Retry-After", "3600")
		http.Error(response, "stored-transfer creation rate exceeded", http.StatusTooManyRequests)
		return
	}
	releaseActive := true
	defer func() {
		if releaseActive {
			s.mu.Lock()
			if s.activeUploads[ip] > 0 {
				s.activeUploads[ip]--
			}
			s.mu.Unlock()
		}
	}()

	var input createRequest
	if err := decodeJSON(request, &input, 64<<10); err != nil {
		http.Error(response, "invalid stored-transfer request", http.StatusBadRequest)
		return
	}
	downloads := DefaultMaxDownloads
	if input.Downloads != nil {
		downloads = *input.Downloads
	}
	expiresSeconds := int64(DefaultExpiration / time.Second)
	if input.ExpiresSeconds != nil {
		expiresSeconds = *input.ExpiresSeconds
	}
	if expiresSeconds < int64(MinExpiration/time.Second) || expiresSeconds > MaxExpirationSeconds {
		http.Error(response, "invalid stored-transfer declaration", http.StatusBadRequest)
		return
	}
	if maximum := int64(s.config.MaxExpiration / time.Second); maximum > 0 && expiresSeconds > maximum {
		expiresSeconds = maximum
	}
	redeemVerifier, err := storecrypto.DecodeBase64URL(input.RedeemVerifier)
	if input.Protocol != storecrypto.Protocol || err != nil || len(redeemVerifier) != sha256.Size ||
		input.ManifestBytes < 29 || input.ManifestBytes > MaxManifestBytes ||
		input.DeclaredFiles < 1 || input.DeclaredFiles > s.config.MaxFiles ||
		input.PlaintextBytes < 0 || input.PlaintextBytes > s.config.MaxTransferBytes ||
		len(input.ChunkBytes) > MaxChunkObjects ||
		downloads < 1 || downloads > s.config.MaxDownloads {
		http.Error(response, "invalid stored-transfer declaration", http.StatusBadRequest)
		return
	}
	var reserved = input.ManifestBytes
	var estimatedPlain int64
	for _, size := range input.ChunkBytes {
		if size < 29 || size > int64(storecrypto.ChunkSize+28) {
			http.Error(response, "invalid stored-transfer chunk declaration", http.StatusBadRequest)
			return
		}
		reserved += size
		estimatedPlain += size - 28
	}
	if estimatedPlain != input.PlaintextBytes || estimatedPlain > s.config.MaxTransferBytes {
		http.Error(response, "stored transfer exceeds the configured limit", http.StatusRequestEntityTooLarge)
		return
	}
	if !s.reserve(reserved) {
		http.Error(response, "stored-transfer quota is full", http.StatusInsufficientStorage)
		return
	}
	keepReservation := false
	defer func() {
		if !keepReservation {
			s.unreserve(reserved)
		}
	}()

	id, err := storecrypto.GenerateTransferID()
	if err != nil {
		http.Error(response, "could not create stored transfer", http.StatusInternalServerError)
		return
	}
	uploadToken, err := randomCapability()
	if err != nil {
		http.Error(response, "could not create stored transfer", http.StatusInternalServerError)
		return
	}
	now := s.now()
	meta := &metadata{
		Version:            storecrypto.Version,
		ID:                 id,
		State:              stateUploading,
		CreatedAt:          now,
		UploadExpiresAt:    now.Add(uploadLifetime),
		ManifestBytes:      input.ManifestBytes,
		ChunkBytes:         append([]int64(nil), input.ChunkBytes...),
		ReservedBytes:      reserved,
		UploadVerifier:     verifier(uploadToken),
		RedeemVerifier:     input.RedeemVerifier,
		DownloadsTotal:     downloads,
		DownloadsRemaining: downloads,
		ExpiresSeconds:     expiresSeconds,
		ClientIP:           ip,
	}
	if err = s.save(meta); err != nil {
		http.Error(response, "could not persist stored transfer", http.StatusInternalServerError)
		return
	}
	keepReservation = true
	releaseActive = false
	response.Header().Set("X-Croc-Downloads", strconv.Itoa(meta.DownloadsTotal))
	writeJSON(response, http.StatusCreated, createResponse{
		ID:              id,
		UploadToken:     storecrypto.EncodeBase64URL(uploadToken),
		UploadExpiresAt: meta.UploadExpiresAt,
		ChunkSize:       storecrypto.ChunkSize,
	})
}

func (s *Service) uploadObject(response http.ResponseWriter, request *http.Request, id string, index int) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || !capabilityMatches(bearer(request), valueOrEmpty(meta, func(m *metadata) string { return m.UploadVerifier })) {
		http.NotFound(response, request)
		return
	}
	if meta.State != stateUploading || !meta.UploadExpiresAt.After(s.now()) {
		http.Error(response, "stored transfer is no longer accepting uploads", http.StatusGone)
		return
	}
	var expected int64
	var destination string
	if index == -1 {
		expected = meta.ManifestBytes
		destination = s.manifestPath(id)
	} else {
		if index < 0 || index >= len(meta.ChunkBytes) {
			http.NotFound(response, request)
			return
		}
		expected = meta.ChunkBytes[index]
		destination = s.chunkPath(id, index)
	}
	if request.ContentLength != expected {
		http.Error(response, "ciphertext length did not match declaration", http.StatusBadRequest)
		return
	}
	digest := strings.TrimSpace(request.Header.Get("X-Croc-SHA256"))
	decodedDigest, digestErr := storecrypto.DecodeBase64URL(digest)
	if digestErr != nil || len(decodedDigest) != sha256.Size {
		http.Error(response, "missing or invalid ciphertext digest", http.StatusBadRequest)
		return
	}
	if err = writeAtomic(destination, expected, digest, request.Body); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func valueOrEmpty(meta *metadata, getter func(*metadata) string) string {
	if meta == nil {
		return ""
	}
	return getter(meta)
}

func (s *Service) complete(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || !capabilityMatches(bearer(request), valueOrEmpty(meta, func(m *metadata) string { return m.UploadVerifier })) {
		http.NotFound(response, request)
		return
	}
	if meta.State == stateAvailable || meta.State == stateClaimed {
		writeJSON(response, http.StatusOK, completeResponse{ExpiresAt: meta.ExpiresAt})
		return
	}
	if meta.State != stateUploading || !meta.UploadExpiresAt.After(s.now()) {
		http.Error(response, "stored transfer is no longer accepting uploads", http.StatusGone)
		return
	}
	if !fileHasSize(s.manifestPath(id), meta.ManifestBytes) {
		http.Error(response, "stored-transfer manifest is missing", http.StatusConflict)
		return
	}
	for index, expected := range meta.ChunkBytes {
		if !fileHasSize(s.chunkPath(id, index), expected) {
			http.Error(response, fmt.Sprintf("stored-transfer chunk %d is missing", index), http.StatusConflict)
			return
		}
	}
	now := s.now()
	meta.State = stateAvailable
	meta.CompletedAt = now
	expiresSeconds := meta.ExpiresSeconds
	if expiresSeconds == 0 {
		// Metadata written before configurable expiration used one day.
		expiresSeconds = int64(DefaultExpiration / time.Second)
	}
	meta.ExpiresAt = now.Add(time.Duration(expiresSeconds) * time.Second)
	if err = s.save(meta); err != nil {
		http.Error(response, "could not finalize stored transfer", http.StatusInternalServerError)
		return
	}
	if meta.ClientIP != "" {
		s.mu.Lock()
		if s.activeUploads[meta.ClientIP] > 0 {
			s.activeUploads[meta.ClientIP]--
		}
		s.mu.Unlock()
	}
	writeJSON(response, http.StatusOK, completeResponse{ExpiresAt: meta.ExpiresAt})
}

func fileHasSize(path string, expected int64) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() == expected
}

func (s *Service) authorizeRedeem(request *http.Request, meta *metadata) bool {
	return capabilityMatches(bearer(request), valueOrEmpty(meta, func(m *metadata) string { return m.RedeemVerifier }))
}

func (s *Service) authorizeClaim(request *http.Request, meta *metadata) bool {
	return capabilityMatches(bearer(request), valueOrEmpty(meta, func(m *metadata) string { return m.ClaimVerifier }))
}

func (s *Service) authorizeCommittedClaim(request *http.Request, meta *metadata) bool {
	if meta == nil {
		return false
	}
	token := bearer(request)
	for _, committed := range meta.CommittedClaims {
		if capabilityMatches(token, committed) {
			return true
		}
	}
	return false
}

func remainingDownloadsHeader(response http.ResponseWriter, remaining int) {
	response.Header().Set("X-Croc-Downloads-Remaining", strconv.Itoa(remaining))
}

func (s *Service) availableState(response http.ResponseWriter, request *http.Request, meta *metadata) bool {
	if isTombstone(meta.State) || meta.DownloadsRemaining < 1 || !meta.ExpiresAt.After(s.now()) {
		if isTombstone(meta.State) {
			_ = s.purgeCiphertext(meta.ID)
		}
		http.Error(response, "stored transfer is no longer available", http.StatusGone)
		return false
	}
	return true
}

func (s *Service) downloadManifest(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	meta, err := s.load(id)
	if err != nil || !s.authorizeRedeem(request, meta) {
		lock.Unlock()
		http.NotFound(response, request)
		return
	}
	if !s.availableState(response, request, meta) {
		lock.Unlock()
		return
	}
	expires := meta.ExpiresAt
	remaining := meta.DownloadsRemaining
	path := s.manifestPath(id)
	lock.Unlock()
	response.Header().Set("Content-Type", "application/octet-stream")
	response.Header().Set("X-Croc-Expires-At", expires.UTC().Format(time.RFC3339))
	remainingDownloadsHeader(response, remaining)
	http.ServeFile(response, request, path)
}

func (s *Service) claim(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || !s.authorizeRedeem(request, meta) {
		http.NotFound(response, request)
		return
	}
	if !s.availableState(response, request, meta) {
		return
	}
	now := s.now()
	if meta.State == stateClaimed && meta.ClaimExpiresAt.After(now) {
		seconds := int64(meta.ClaimExpiresAt.Sub(now).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		response.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		http.Error(response, "stored transfer is already claimed", http.StatusLocked)
		return
	}
	token, err := randomCapability()
	if err != nil {
		http.Error(response, "could not claim stored transfer", http.StatusInternalServerError)
		return
	}
	meta.State = stateClaimed
	meta.ClaimVerifier = verifier(token)
	meta.ClaimExpiresAt = minTime(now.Add(claimLifetime), meta.ExpiresAt)
	if err = s.save(meta); err != nil {
		http.Error(response, "could not persist stored-transfer claim", http.StatusInternalServerError)
		return
	}
	remainingDownloadsHeader(response, meta.DownloadsRemaining)
	writeJSON(response, http.StatusCreated, claimResponse{
		ClaimToken:     storecrypto.EncodeBase64URL(token),
		ClaimExpiresAt: meta.ClaimExpiresAt,
		ExpiresAt:      meta.ExpiresAt,
	})
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (s *Service) downloadChunk(response http.ResponseWriter, request *http.Request, id string, index int) {
	lock := s.lockFor(id)
	lock.Lock()
	meta, err := s.load(id)
	if err != nil || meta == nil || meta.State != stateClaimed || !s.authorizeClaim(request, meta) {
		lock.Unlock()
		http.NotFound(response, request)
		return
	}
	if !s.availableState(response, request, meta) {
		lock.Unlock()
		return
	}
	if !meta.ClaimExpiresAt.After(s.now()) {
		lock.Unlock()
		http.Error(response, "stored-transfer claim expired", http.StatusGone)
		return
	}
	if index < 0 || index >= len(meta.ChunkBytes) {
		lock.Unlock()
		http.NotFound(response, request)
		return
	}
	meta.ClaimExpiresAt = minTime(s.now().Add(claimLifetime), meta.ExpiresAt)
	if err = s.save(meta); err != nil {
		lock.Unlock()
		http.Error(response, "could not renew stored-transfer claim", http.StatusInternalServerError)
		return
	}
	path := s.chunkPath(id, index)
	lock.Unlock()
	response.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(response, request, path)
}

func (s *Service) release(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || meta == nil || meta.State != stateClaimed || !s.authorizeClaim(request, meta) {
		http.NotFound(response, request)
		return
	}
	if !s.availableState(response, request, meta) {
		return
	}
	meta.State = stateAvailable
	meta.ClaimVerifier = ""
	meta.ClaimExpiresAt = time.Time{}
	if err = s.save(meta); err != nil {
		http.Error(response, "could not release stored-transfer claim", http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Service) commit(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || meta == nil {
		http.NotFound(response, request)
		return
	}
	if s.authorizeCommittedClaim(request, meta) {
		remainingDownloadsHeader(response, meta.DownloadsRemaining)
		if meta.State != stateConsumed {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if err = s.purgeCiphertext(meta.ID); err != nil {
			http.Error(response, "could not remove stored ciphertext", http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorizeClaim(request, meta) {
		http.NotFound(response, request)
		return
	}
	if meta.State != stateClaimed || !s.availableState(response, request, meta) {
		return
	}
	if !meta.ClaimExpiresAt.After(s.now()) {
		http.Error(response, "stored-transfer claim expired", http.StatusGone)
		return
	}
	meta.CommittedClaims = append(meta.CommittedClaims, meta.ClaimVerifier)
	meta.DownloadsRemaining--
	meta.State = stateAvailable
	meta.ClaimVerifier = ""
	meta.ClaimExpiresAt = time.Time{}
	if meta.DownloadsRemaining == 0 {
		if err = s.tombstone(meta, stateConsumed); err != nil {
			http.Error(response, "could not consume stored transfer", http.StatusInternalServerError)
			return
		}
	} else if err = s.save(meta); err != nil {
		http.Error(response, "could not commit stored download", http.StatusInternalServerError)
		return
	}
	remainingDownloadsHeader(response, meta.DownloadsRemaining)
	response.WriteHeader(http.StatusNoContent)
}

func (s *Service) revoke(response http.ResponseWriter, request *http.Request, id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	meta, err := s.load(id)
	if err != nil || !capabilityMatches(bearer(request), valueOrEmpty(meta, func(m *metadata) string { return m.UploadVerifier })) {
		http.NotFound(response, request)
		return
	}
	if meta.State == stateRevoked {
		if err = s.purgeCiphertext(meta.ID); err != nil {
			http.Error(response, "could not remove stored ciphertext", http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if meta.State == stateConsumed || meta.State == stateExpired {
		http.Error(response, "stored transfer is no longer available", http.StatusGone)
		return
	}
	if err = s.tombstone(meta, stateRevoked); err != nil {
		http.Error(response, "could not revoke stored transfer", http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
