// Package storeclient implements the client side of croc-store-v1 for the CLI.
package storeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/croc/v11/src/storecrypto"
)

const maxJSONResponse = 1 << 20

const (
	storedTransferWorkers = 4
	checkpointChunks      = 8
)

// Client talks to one croc stored-transfer HTTP service.
type Client struct {
	HTTP *http.Client
}

// Progress reports plaintext bytes processed.
type Progress struct {
	FileIndex  int
	FileCount  int
	FileName   string
	FileBytes  int64
	FileSize   int64
	TotalBytes int64
	TotalSize  int64
}

// Callbacks receives human-readable status and byte progress.
type Callbacks struct {
	Status   func(string)
	Progress func(Progress)
}

// UploadResult contains the share and sender-only revocation capability.
type UploadResult struct {
	Share       storecrypto.Share
	UploadToken string
	ExpiresAt   time.Time
	Downloads   int
}

// UploadOptions configures a stored upload. Zero values select the historical
// defaults of one verified download and one day of storage.
type UploadOptions struct {
	Downloads  int
	Expiration time.Duration
}

type createRequest struct {
	Protocol       string  `json:"protocol"`
	ManifestBytes  int64   `json:"manifestBytes"`
	ChunkBytes     []int64 `json:"chunkBytes"`
	RedeemVerifier string  `json:"redeemVerifier"`
	Files          int     `json:"files"`
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

type completeResponse struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

type claimResponse struct {
	ClaimToken     string    `json:"claimToken"`
	ClaimExpiresAt time.Time `json:"claimExpiresAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type downloadState struct {
	Version      int             `json:"version"`
	ID           string          `json:"id"`
	ManifestHash string          `json:"manifestHash"`
	ClaimToken   string          `json:"claimToken"`
	Completed    map[int]bool    `json:"completed"`
	Renamed      map[string]bool `json:"renamed"`
	Verified     bool            `json:"verified"`
}

type downloadSession struct {
	share           storecrypto.Share
	outputDirectory string
	statePath       string
	state           downloadState
	refs            []storecrypto.ChunkRef
	fileCount       int
	transferred     int64
	total           int64
	callbacks       Callbacks
	stateMu         sync.Mutex
	claimMu         sync.Mutex
	checkpointCount int
}

// HTTPError represents a non-success response from the storage service.
type HTTPError struct {
	StatusCode int
	Message    string
	RetryAfter string
}

func (e *HTTPError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("stored-transfer service returned HTTP %d", e.StatusCode)
	}
	return strings.TrimSpace(e.Message)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("stored-transfer redirects are not allowed")
		},
	}
}

func apiURL(origin, suffix string) string {
	return strings.TrimSuffix(origin, "/") + "/api/v1/store/transfers" + suffix
}

func (c *Client) do(request *http.Request) (*http.Response, error) {
	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	return nil, &HTTPError{
		StatusCode: response.StatusCode,
		Message:    string(body),
		RetryAfter: response.Header.Get("Retry-After"),
	}
}

func jsonRequest(ctx context.Context, method, target, token string, body any) (*http.Request, error) {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func decodeResponse(response *http.Response, value any) error {
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxJSONResponse))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("stored-transfer service response has trailing JSON")
	}
	return nil
}

func status(callbacks Callbacks, value string) {
	if callbacks.Status != nil {
		callbacks.Status(value)
	}
}

func progress(callbacks Callbacks, value Progress) {
	if callbacks.Progress != nil {
		callbacks.Progress(value)
	}
}

type uploadFile struct {
	path     string
	info     os.FileInfo
	manifest storecrypto.ManifestFile
}

type preparedUpload struct {
	files        []uploadFile
	manifest     storecrypto.Manifest
	manifestJSON []byte
	chunkBytes   []int64
	totalBytes   int64
}

// Upload hashes, encrypts, and uploads regular files. Upload sessions are not
// persisted; a terminal failure revokes the partial transfer.
func (c *Client) Upload(
	ctx context.Context,
	origin string,
	paths []string,
	callbacks Callbacks,
) (result UploadResult, err error) {
	return c.UploadWithOptions(ctx, origin, paths, UploadOptions{}, callbacks)
}

// UploadWithDownloads uploads regular files with a requested verified-download
// allowance. Servers reject values above their configured maximum.
func (c *Client) UploadWithDownloads(
	ctx context.Context,
	origin string,
	paths []string,
	downloads int,
	callbacks Callbacks,
) (result UploadResult, err error) {
	if downloads < 1 {
		return result, errors.New("stored-transfer downloads must be positive")
	}
	return c.UploadWithOptions(ctx, origin, paths, UploadOptions{Downloads: downloads}, callbacks)
}

// UploadWithOptions uploads regular files with a requested verified-download
// allowance and finite storage lifetime.
func (c *Client) UploadWithOptions(
	ctx context.Context,
	origin string,
	paths []string,
	options UploadOptions,
	callbacks Callbacks,
) (result UploadResult, err error) {
	if err = validateOrigin(origin); err != nil {
		return result, err
	}
	if options.Downloads == 0 {
		options.Downloads = 1
	}
	if options.Downloads < 1 {
		return result, errors.New("stored-transfer downloads must be positive")
	}
	if options.Expiration == 0 {
		options.Expiration = 24 * time.Hour
	}
	if options.Expiration < time.Minute {
		return result, errors.New("stored-transfer expiration must be at least one minute")
	}
	if options.Expiration%time.Second != 0 {
		return result, errors.New("stored-transfer expiration must use whole seconds")
	}
	prepared, err := prepareUpload(ctx, paths, callbacks)
	if err != nil {
		return result, err
	}
	master, err := storecrypto.GenerateKey()
	if err != nil {
		return result, err
	}
	result, err = c.createUploadWithOptions(ctx, origin, master, prepared, options, callbacks)
	if err != nil {
		return result, err
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		revokeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Revoke(revokeCtx, result.Share, result.UploadToken)
	}()

	if err = c.uploadObjects(ctx, prepared, result, callbacks); err != nil {
		return result, err
	}
	result.ExpiresAt, err = c.completeUpload(ctx, result, callbacks)
	if err != nil {
		return result, err
	}
	completed = true
	status(callbacks, "Encrypted upload complete")
	return result, nil
}

func validateOrigin(origin string) error {
	probe := storecrypto.Share{
		Origin:    origin,
		ID:        storecrypto.EncodeBase64URL(make([]byte, storecrypto.TransferIDLen)),
		MasterKey: make([]byte, storecrypto.KeySize),
	}
	_, err := probe.BrowserURL()
	return err
}

func prepareUpload(
	ctx context.Context,
	paths []string,
	callbacks Callbacks,
) (preparedUpload, error) {
	if len(paths) == 0 || len(paths) > storecrypto.MaxFiles {
		return preparedUpload{}, fmt.Errorf(
			"stored transfer must contain between 1 and %d files",
			storecrypto.MaxFiles,
		)
	}
	prepared := preparedUpload{
		files: make([]uploadFile, 0, len(paths)),
	}
	names := make(map[string]struct{}, len(paths))
	nextChunk := 0
	for index, filePath := range paths {
		info, err := os.Lstat(filePath)
		if err != nil {
			return preparedUpload{}, err
		}
		if !info.Mode().IsRegular() {
			return preparedUpload{}, fmt.Errorf(
				"stored mode supports regular files only: %s",
				filePath,
			)
		}
		name := filepath.Base(filePath)
		if _, exists := names[name]; exists {
			return preparedUpload{}, fmt.Errorf(
				"duplicate stored-transfer filename: %s",
				name,
			)
		}
		names[name] = struct{}{}
		prepared.totalBytes += info.Size()
		status(callbacks, fmt.Sprintf("Hashing %d/%d: %s", index+1, len(paths), name))
		hash, err := hashFile(ctx, filePath)
		if err != nil {
			return preparedUpload{}, err
		}
		chunks := int((info.Size() + storecrypto.ChunkSize - 1) / storecrypto.ChunkSize)
		prepared.files = append(prepared.files, uploadFile{
			path: filePath,
			info: info,
			manifest: storecrypto.ManifestFile{
				Name:       name,
				Size:       info.Size(),
				Modified:   info.ModTime().UTC(),
				SHA256:     storecrypto.EncodeBase64URL(hash),
				FirstChunk: nextChunk,
				ChunkCount: chunks,
			},
		})
		nextChunk += chunks
	}

	prepared.manifest = storecrypto.Manifest{
		Version:   storecrypto.Version,
		ChunkSize: storecrypto.ChunkSize,
		Files:     make([]storecrypto.ManifestFile, len(prepared.files)),
	}
	for index, file := range prepared.files {
		prepared.manifest.Files[index] = file.manifest
		remaining := file.info.Size()
		for chunk := 0; chunk < file.manifest.ChunkCount; chunk++ {
			size := int64(storecrypto.ChunkSize)
			if remaining < size {
				size = remaining
			}
			prepared.chunkBytes = append(prepared.chunkBytes, size+28)
			remaining -= size
		}
	}
	if err := storecrypto.ValidateManifest(prepared.manifest, 1<<40); err != nil {
		return preparedUpload{}, err
	}
	manifestJSON, err := json.Marshal(prepared.manifest)
	if err != nil {
		return preparedUpload{}, err
	}
	prepared.manifestJSON = manifestJSON
	return prepared, nil
}

func (c *Client) createUpload(
	ctx context.Context,
	origin string,
	master []byte,
	prepared preparedUpload,
	downloads int,
	callbacks Callbacks,
) (UploadResult, error) {
	return c.createUploadWithOptions(ctx, origin, master, prepared, UploadOptions{
		Downloads:  downloads,
		Expiration: 24 * time.Hour,
	}, callbacks)
}

func (c *Client) createUploadWithOptions(
	ctx context.Context,
	origin string,
	master []byte,
	prepared preparedUpload,
	options UploadOptions,
	callbacks Callbacks,
) (UploadResult, error) {
	redeem, err := storecrypto.RedeemCapability(master)
	if err != nil {
		return UploadResult{}, err
	}
	var requestedDownloads *int
	if options.Downloads != 1 {
		requestedDownloads = &options.Downloads
	}
	var requestedExpiration *int64
	if options.Expiration != 24*time.Hour {
		seconds := int64(options.Expiration / time.Second)
		requestedExpiration = &seconds
	}
	create := createRequest{
		Protocol:       storecrypto.Protocol,
		ManifestBytes:  int64(len(prepared.manifestJSON) + 28),
		ChunkBytes:     prepared.chunkBytes,
		RedeemVerifier: storecrypto.EncodeBase64URL(storecrypto.CapabilityVerifier(redeem)),
		Files:          len(prepared.files),
		PlaintextBytes: prepared.totalBytes,
		Downloads:      requestedDownloads,
		ExpiresSeconds: requestedExpiration,
	}
	status(callbacks, "Reserving encrypted temporary storage…")
	request, err := jsonRequest(ctx, http.MethodPost, apiURL(origin, ""), "", create)
	if err != nil {
		return UploadResult{}, err
	}
	response, err := c.do(request)
	if err != nil {
		return UploadResult{}, err
	}
	acceptedDownloads := 1
	if header := strings.TrimSpace(response.Header.Get("X-Croc-Downloads")); header != "" {
		acceptedDownloads, err = strconv.Atoi(header)
		if err != nil || acceptedDownloads < 1 {
			response.Body.Close()
			return UploadResult{}, errors.New("storage service returned an invalid download count")
		}
	}
	var created createResponse
	if err := decodeResponse(response, &created); err != nil {
		return UploadResult{}, err
	}
	if created.ChunkSize != storecrypto.ChunkSize {
		return UploadResult{}, fmt.Errorf(
			"storage service returned unsupported chunk size %d",
			created.ChunkSize,
		)
	}
	if acceptedDownloads != options.Downloads {
		return UploadResult{}, fmt.Errorf(
			"storage service created %d downloads instead of %d",
			acceptedDownloads,
			options.Downloads,
		)
	}
	result := UploadResult{
		Share: storecrypto.Share{
			Origin:    origin,
			ID:        created.ID,
			MasterKey: master,
		},
		UploadToken: created.UploadToken,
		Downloads:   acceptedDownloads,
	}
	if _, err := result.Share.BrowserURL(); err != nil {
		return result, fmt.Errorf(
			"storage service returned an invalid transfer id: %w",
			err,
		)
	}
	if !validCapability(result.UploadToken) {
		return result, errors.New("storage service returned an invalid upload capability")
	}
	return result, nil
}

func validCapability(value string) bool {
	capability, err := storecrypto.DecodeBase64URL(value)
	return err == nil &&
		len(capability) == 32 &&
		storecrypto.EncodeBase64URL(capability) == value
}

func (c *Client) uploadObjects(
	ctx context.Context,
	prepared preparedUpload,
	result UploadResult,
	callbacks Callbacks,
) error {
	encryptedManifest, err := storecrypto.SealManifest(
		result.Share.MasterKey,
		result.Share.ID,
		prepared.manifest,
	)
	if err != nil {
		return err
	}
	status(callbacks, "Uploading encrypted manifest…")
	if err := c.putWithRetry(
		ctx,
		apiURL(result.Share.Origin, "/"+result.Share.ID+"/manifest"),
		result.UploadToken,
		encryptedManifest,
	); err != nil {
		return err
	}

	var sent int64
	refs := storecrypto.ChunkRefs(prepared.manifest)
	for fileIndex, file := range prepared.files {
		sent, err = c.uploadFile(
			ctx,
			result,
			file,
			fileIndex,
			len(prepared.files),
			refs,
			sent,
			prepared.totalBytes,
			callbacks,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) uploadFile(
	ctx context.Context,
	result UploadResult,
	file uploadFile,
	fileIndex int,
	fileCount int,
	refs []storecrypto.ChunkRef,
	sent int64,
	total int64,
	callbacks Callbacks,
) (int64, error) {
	handle, err := os.Open(file.path)
	if err != nil {
		return sent, err
	}
	defer handle.Close()
	workerCount := min(storedTransferWorkers, file.manifest.ChunkCount)
	if workerCount == 0 {
		return sent, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var next atomic.Int64
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	var firstErr error
	var fileSent int64
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunkCipher, cipherErr := storecrypto.NewChunkCipher(result.Share.MasterKey)
			if cipherErr != nil {
				resultMu.Lock()
				if firstErr == nil {
					firstErr = cipherErr
					cancel()
				}
				resultMu.Unlock()
				return
			}
			chunkBuffer := make([]byte, storecrypto.ChunkSize+32)
			nonceSize := chunkCipher.NonceSize()
			for {
				fileChunk := int(next.Add(1) - 1)
				if fileChunk >= file.manifest.ChunkCount || workCtx.Err() != nil {
					return
				}
				ref := refs[file.manifest.FirstChunk+fileChunk]
				plaintext := chunkBuffer[nonceSize : nonceSize+ref.PlainSize]
				if _, readErr := handle.ReadAt(plaintext, int64(fileChunk)*storecrypto.ChunkSize); readErr != nil {
					resultMu.Lock()
					if firstErr == nil {
						firstErr = readErr
						cancel()
					}
					resultMu.Unlock()
					return
				}
				ciphertext, sealErr := chunkCipher.SealInPlace(chunkBuffer, result.Share.ID, ref)
				if sealErr == nil {
					resultMu.Lock()
					status(callbacks, "Uploading "+file.manifest.Name)
					resultMu.Unlock()
					sealErr = c.putWithRetry(
						workCtx,
						apiURL(result.Share.Origin, fmt.Sprintf("/%s/chunks/%d", result.Share.ID, ref.ObjectIndex)),
						result.UploadToken,
						ciphertext,
					)
				}
				if sealErr != nil {
					resultMu.Lock()
					if firstErr == nil {
						firstErr = sealErr
						cancel()
					}
					resultMu.Unlock()
					return
				}
				resultMu.Lock()
				fileSent += int64(ref.PlainSize)
				sent += int64(ref.PlainSize)
				progress(callbacks, Progress{
					FileIndex: fileIndex, FileCount: fileCount, FileName: file.manifest.Name,
					FileBytes: fileSent, FileSize: file.manifest.Size,
					TotalBytes: sent, TotalSize: total,
				})
				resultMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return sent, firstErr
}

func (c *Client) completeUpload(
	ctx context.Context,
	result UploadResult,
	callbacks Callbacks,
) (time.Time, error) {
	status(callbacks, "Finalizing encrypted transfer…")
	request, err := jsonRequest(
		ctx,
		http.MethodPost,
		apiURL(result.Share.Origin, "/"+result.Share.ID+"/complete"),
		result.UploadToken,
		nil,
	)
	if err != nil {
		return time.Time{}, err
	}
	response, err := c.do(request)
	if err != nil {
		return time.Time{}, err
	}
	var finished completeResponse
	if err := decodeResponse(response, &finished); err != nil {
		return time.Time{}, err
	}
	if finished.ExpiresAt.IsZero() {
		return time.Time{}, errors.New("storage service returned an invalid expiration time")
	}
	return finished.ExpiresAt, nil
}

func hashFile(ctx context.Context, path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 256<<10)
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			return hash.Sum(nil), nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func (c *Client) putWithRetry(ctx context.Context, target, token string, payload []byte) error {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		request.ContentLength = int64(len(payload))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Croc-SHA256", storecrypto.EncodedSHA256(payload))
		response, requestErr := c.do(request)
		if requestErr == nil {
			response.Body.Close()
			return nil
		}
		last = requestErr
		var httpErr *HTTPError
		if errors.As(requestErr, &httpErr) && httpErr.StatusCode < 500 {
			return requestErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
	return last
}

// Inspect fetches and decrypts a manifest without claiming the transfer.
func (c *Client) Inspect(ctx context.Context, share storecrypto.Share) (storecrypto.Manifest, time.Time, error) {
	if _, err := share.BrowserURL(); err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	redeem, err := storecrypto.RedeemCapability(share.MasterKey)
	if err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	request, err := jsonRequest(
		ctx,
		http.MethodGet,
		apiURL(share.Origin, "/"+share.ID+"/manifest"),
		storecrypto.EncodeBase64URL(redeem),
		nil,
	)
	if err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	response, err := c.do(request)
	if err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	defer response.Body.Close()
	ciphertext, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
	if err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	manifest, err := storecrypto.OpenManifest(
		share.MasterKey,
		share.ID,
		ciphertext,
		1<<40,
	)
	if err != nil {
		return storecrypto.Manifest{}, time.Time{}, err
	}
	expires, _ := time.Parse(time.RFC3339, response.Header.Get("X-Croc-Expires-At"))
	return manifest, expires, nil
}

// Receive claims, downloads, verifies, and atomically installs all files.
func (c *Client) Receive(
	ctx context.Context,
	share storecrypto.Share,
	manifest storecrypto.Manifest,
	outputDirectory string,
	callbacks Callbacks,
) error {
	if _, err := share.BrowserURL(); err != nil {
		return err
	}
	if err := storecrypto.ValidateManifest(manifest, 1<<40); err != nil {
		return err
	}
	session, err := c.startDownload(
		ctx,
		share,
		manifest,
		outputDirectory,
		callbacks,
	)
	if err != nil {
		return err
	}
	for fileIndex, file := range manifest.Files {
		if session.state.Renamed[file.Name] {
			session.transferred += file.Size
			continue
		}
		if err = c.receiveFile(ctx, session, file, fileIndex); err != nil {
			return err
		}
	}
	session.state.Verified = true
	if err = writeState(session.statePath, session.state); err != nil {
		return err
	}
	status(callbacks, "Committing verified download…")
	remaining, err := c.commitWithClaimRetry(ctx, session)
	if err != nil {
		return err
	}
	if remaining == 0 {
		status(callbacks, "Verified download committed; stored ciphertext removed")
	} else if remaining == 1 {
		status(callbacks, "Verified download committed; one download remains")
	} else {
		status(callbacks, fmt.Sprintf("Verified download committed; %d downloads remain", remaining))
	}
	return os.Remove(session.statePath)
}

func (c *Client) startDownload(
	ctx context.Context,
	share storecrypto.Share,
	manifest storecrypto.Manifest,
	outputDirectory string,
	callbacks Callbacks,
) (*downloadSession, error) {
	if outputDirectory == "" {
		outputDirectory = "."
	}
	absoluteOutput, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(absoluteOutput, 0o755); err != nil {
		return nil, err
	}
	statePath := filepath.Join(absoluteOutput, ".croc-store-"+share.ID+".json")
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	state := downloadState{
		Version:      storecrypto.Version,
		ID:           share.ID,
		ManifestHash: storecrypto.EncodedSHA256(manifestBytes),
		Completed:    make(map[int]bool),
		Renamed:      make(map[string]bool),
	}
	if existing, readErr := readDownloadState(statePath); readErr == nil &&
		existing.ID == state.ID &&
		existing.ManifestHash == state.ManifestHash {
		state = existing
	}
	if state.ClaimToken == "" {
		token, claimErr := c.claim(ctx, share)
		if claimErr != nil {
			return nil, claimErr
		}
		state.ClaimToken = token
		if err = writeState(statePath, state); err != nil {
			return nil, err
		}
	}
	return &downloadSession{
		share:           share,
		outputDirectory: absoluteOutput,
		statePath:       statePath,
		state:           state,
		refs:            storecrypto.ChunkRefs(manifest),
		fileCount:       len(manifest.Files),
		total:           manifestSize(manifest),
		callbacks:       callbacks,
	}, nil
}

func manifestSize(manifest storecrypto.Manifest) int64 {
	var total int64
	for _, file := range manifest.Files {
		total += file.Size
	}
	return total
}

func (c *Client) receiveFile(
	ctx context.Context,
	session *downloadSession,
	file storecrypto.ManifestFile,
	fileIndex int,
) error {
	partPath := filepath.Join(
		session.outputDirectory,
		"."+file.Name+".croc-"+session.share.ID+".part",
	)
	handle, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err = handle.Truncate(file.Size); err != nil {
		handle.Close()
		return err
	}
	err = c.writeFileChunks(ctx, session, file, fileIndex, handle)
	if err != nil {
		handle.Close()
		return err
	}
	if err = handle.Sync(); err != nil {
		handle.Close()
		return err
	}
	if err = handle.Close(); err != nil {
		return err
	}
	if err = installVerifiedFile(
		ctx,
		file,
		partPath,
		session.outputDirectory,
		session.callbacks,
	); err != nil {
		return err
	}
	session.state.Renamed[file.Name] = true
	return writeState(session.statePath, session.state)
}

func (c *Client) writeFileChunks(
	ctx context.Context,
	session *downloadSession,
	file storecrypto.ManifestFile,
	fileIndex int,
	handle *os.File,
) error {
	var fileBytes int64
	type chunkJob struct {
		chunk int
		ref   storecrypto.ChunkRef
	}
	pending := make([]chunkJob, 0, file.ChunkCount)
	for chunk := 0; chunk < file.ChunkCount; chunk++ {
		ref := session.refs[file.FirstChunk+chunk]
		if session.state.Completed[ref.ObjectIndex] {
			fileBytes += int64(ref.PlainSize)
			session.transferred += int64(ref.PlainSize)
			continue
		}
		pending = append(pending, chunkJob{chunk: chunk, ref: ref})
	}
	workerCount := min(storedTransferWorkers, len(pending))
	if workerCount == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var next atomic.Int64
	var wg sync.WaitGroup
	var firstErr error
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunkCipher, cipherErr := storecrypto.NewChunkCipher(session.share.MasterKey)
			if cipherErr != nil {
				session.stateMu.Lock()
				if firstErr == nil {
					firstErr = cipherErr
					cancel()
				}
				session.stateMu.Unlock()
				return
			}
			for {
				jobIndex := int(next.Add(1) - 1)
				if jobIndex >= len(pending) || workCtx.Err() != nil {
					return
				}
				job := pending[jobIndex]
				session.stateMu.Lock()
				status(session.callbacks, "Downloading "+file.Name)
				session.stateMu.Unlock()
				ciphertext, downloadErr := c.getChunkWithClaimRetry(workCtx, session, job.ref.ObjectIndex)
				var plaintext []byte
				if downloadErr == nil {
					plaintext, downloadErr = chunkCipher.OpenInPlace(session.share.ID, job.ref, ciphertext)
				}
				if downloadErr == nil {
					_, downloadErr = handle.WriteAt(plaintext, int64(job.chunk)*storecrypto.ChunkSize)
				}
				session.stateMu.Lock()
				if downloadErr != nil {
					if firstErr == nil {
						firstErr = downloadErr
						cancel()
					}
					session.stateMu.Unlock()
					return
				}
				session.state.Completed[job.ref.ObjectIndex] = true
				session.checkpointCount++
				fileBytes += int64(len(plaintext))
				session.transferred += int64(len(plaintext))
				if session.checkpointCount >= checkpointChunks {
					downloadErr = writeState(session.statePath, session.state)
					session.checkpointCount = 0
				}
				progress(session.callbacks, Progress{
					FileIndex: fileIndex, FileCount: session.fileCount, FileName: file.Name,
					FileBytes: fileBytes, FileSize: file.Size,
					TotalBytes: session.transferred, TotalSize: session.total,
				})
				if downloadErr != nil && firstErr == nil {
					firstErr = downloadErr
					cancel()
				}
				session.stateMu.Unlock()
				if downloadErr != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	session.stateMu.Lock()
	err := writeState(session.statePath, session.state)
	session.checkpointCount = 0
	session.stateMu.Unlock()
	return err
}

func installVerifiedFile(
	ctx context.Context,
	file storecrypto.ManifestFile,
	partPath string,
	outputDirectory string,
	callbacks Callbacks,
) error {
	status(callbacks, "Verifying "+file.Name)
	hash, err := hashFile(ctx, partPath)
	if err != nil {
		return err
	}
	if storecrypto.EncodeBase64URL(hash) != file.SHA256 {
		return fmt.Errorf("stored-transfer hash verification failed for %s", file.Name)
	}
	if err = os.Chmod(partPath, 0o600); err != nil {
		return err
	}
	if err = os.Chtimes(partPath, time.Now(), file.Modified); err != nil {
		return err
	}
	return os.Rename(partPath, filepath.Join(outputDirectory, file.Name))
}

func expiredClaim(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) &&
		(httpErr.StatusCode == http.StatusNotFound ||
			httpErr.StatusCode == http.StatusGone)
}

func (c *Client) renewClaim(
	ctx context.Context,
	session *downloadSession,
	staleToken string,
) error {
	session.claimMu.Lock()
	defer session.claimMu.Unlock()
	session.stateMu.Lock()
	if session.state.ClaimToken != staleToken {
		session.stateMu.Unlock()
		return nil
	}
	session.stateMu.Unlock()
	token, err := c.claim(ctx, session.share)
	if err != nil {
		return err
	}
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	session.state.ClaimToken = token
	return writeState(session.statePath, session.state)
}

func withFreshClaim[T any](
	ctx context.Context,
	client *Client,
	session *downloadSession,
	operation func(string) (T, error),
) (T, error) {
	session.stateMu.Lock()
	token := session.state.ClaimToken
	session.stateMu.Unlock()
	value, err := operation(token)
	if err == nil || !expiredClaim(err) {
		return value, err
	}
	if err = client.renewClaim(ctx, session, token); err != nil {
		var zero T
		return zero, err
	}
	session.stateMu.Lock()
	token = session.state.ClaimToken
	session.stateMu.Unlock()
	return operation(token)
}

func (c *Client) getChunkWithClaimRetry(
	ctx context.Context,
	session *downloadSession,
	index int,
) ([]byte, error) {
	return withFreshClaim(ctx, c, session, func(token string) ([]byte, error) {
		return c.getChunk(ctx, session.share, token, index)
	})
}

func (c *Client) commitWithClaimRetry(
	ctx context.Context,
	session *downloadSession,
) (int, error) {
	return withFreshClaim(ctx, c, session, func(token string) (int, error) {
		return c.commit(ctx, session.share, token)
	})
}

func readDownloadState(path string) (downloadState, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return downloadState{}, err
	}
	var state downloadState
	err = json.Unmarshal(bytes, &state)
	if state.Completed == nil {
		state.Completed = make(map[int]bool)
	}
	if state.Renamed == nil {
		state.Renamed = make(map[string]bool)
	}
	return state, err
}

func writeState(path string, state downloadState) error {
	bytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".croc-store-state-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	_ = temp.Chmod(0o600)
	if _, err = temp.Write(bytes); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (c *Client) claim(ctx context.Context, share storecrypto.Share) (string, error) {
	redeem, err := storecrypto.RedeemCapability(share.MasterKey)
	if err != nil {
		return "", err
	}
	request, err := jsonRequest(
		ctx,
		http.MethodPost,
		apiURL(share.Origin, "/"+share.ID+"/claim"),
		storecrypto.EncodeBase64URL(redeem),
		nil,
	)
	if err != nil {
		return "", err
	}
	response, err := c.do(request)
	if err != nil {
		return "", err
	}
	var claimed claimResponse
	if err = decodeResponse(response, &claimed); err != nil {
		return "", err
	}
	if !validCapability(claimed.ClaimToken) {
		return "", errors.New("storage service returned an invalid claim capability")
	}
	return claimed.ClaimToken, nil
}

func (c *Client) getChunk(
	ctx context.Context,
	share storecrypto.Share,
	claimToken string,
	index int,
) ([]byte, error) {
	request, err := jsonRequest(
		ctx,
		http.MethodGet,
		apiURL(share.Origin, fmt.Sprintf("/%s/chunks/%d", share.ID, index)),
		claimToken,
		nil,
	)
	if err != nil {
		return nil, err
	}
	response, err := c.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(io.LimitReader(response.Body, storecrypto.ChunkSize+29))
}

func (c *Client) commit(ctx context.Context, share storecrypto.Share, claimToken string) (int, error) {
	request, err := jsonRequest(
		ctx,
		http.MethodPost,
		apiURL(share.Origin, "/"+share.ID+"/commit"),
		claimToken,
		nil,
	)
	if err != nil {
		return 0, err
	}
	response, err := c.do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	header := strings.TrimSpace(response.Header.Get("X-Croc-Downloads-Remaining"))
	if header == "" {
		// A compatible older server always consumes its sole download.
		return 0, nil
	}
	remaining, parseErr := strconv.Atoi(header)
	if parseErr != nil || remaining < 0 {
		return 0, errors.New("storage service returned an invalid remaining-download count")
	}
	return remaining, nil
}

// Revoke deletes an incomplete or available transfer using the sender receipt.
func (c *Client) Revoke(
	ctx context.Context,
	share storecrypto.Share,
	uploadToken string,
) error {
	if _, err := share.BrowserURL(); err != nil {
		return err
	}
	request, err := jsonRequest(
		ctx,
		http.MethodDelete,
		apiURL(share.Origin, "/"+share.ID),
		uploadToken,
		nil,
	)
	if err != nil {
		return err
	}
	response, err := c.do(request)
	if err == nil {
		response.Body.Close()
	}
	return err
}

// IsStoredValue reports whether input looks like a stored URL or CLI token.
func IsStoredValue(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, storecrypto.Protocol+".") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && strings.HasPrefix(parsed.Path, "/s/")
}
