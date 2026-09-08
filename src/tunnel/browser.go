//go:build !js

package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
)

type BrowserRequest struct {
	Path      string      `json:"path"`
	Method    string      `json:"method,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Protocols []string    `json:"protocols,omitempty"`
}
type BrowserResponse struct {
	Status   int         `json:"status,omitempty"`
	Headers  http.Header `json:"headers,omitempty"`
	Protocol string      `json:"protocol,omitempty"`
}

func validateBrowserRequest(metadata []byte) (BrowserRequest, error) {
	var r BrowserRequest
	if len(metadata) > maxControl || json.Unmarshal(metadata, &r) != nil {
		return r, errors.New("invalid browser request")
	}
	u, err := url.ParseRequestURI(r.Path)
	if err != nil || !strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") || u.IsAbs() || u.Host != "" || strings.ContainsAny(r.Path, "\r\n\\") {
		return r, errors.New("request must stay on the shared service")
	}
	if len(r.Protocols) > 16 {
		return r, errors.New("too many WebSocket protocols")
	}
	return r, nil
}
func (h *Host) handleChannel(ctx context.Context, incoming ssh.NewChannel) {
	kind := incoming.ChannelType()
	var request BrowserRequest
	if kind != channelTCP {
		var err error
		request, err = validateBrowserRequest(incoming.ExtraData())
		if err != nil {
			_ = incoming.Reject(ssh.Prohibited, err.Error())
			return
		}
	}
	ch, requests, err := incoming.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { ssh.DiscardRequests(requests); cancel() }()
	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()
	if kind == channelTCP {
		upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", h.target)
		if err != nil {
			return
		}
		copyDuplex(ctx, ch, upstream.(*net.TCPConn))
		return
	}
	if kind == ChannelHTTP {
		err = h.serveHTTP(ctx, ch, request)
	} else {
		err = h.serveWebSocket(ctx, ch, request)
	}
	if err != nil {
		_ = WriteRecord(ch, RecordError, []byte("Local service request failed: "+safeRequestError(err)))
	}
}
func safeRequestError(err error) string {
	// Request URLs, query tokens and request bodies must not be reflected in
	// infrastructure logs. This message goes only to the authenticated guest.
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return "connection unavailable"
	}
	return "invalid or interrupted response"
}
func (h *Host) httpClient() *http.Client {
	transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: MaxRecord, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", h.target)
	}}
	// Return redirects to the browser bridge for validation and navigation.
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (h *Host) requestURL(path string) string {
	return "http://localhost:" + strconv.Itoa(h.config.Port) + path
}
func cleanHeaders(headers http.Header) http.Header {
	result := make(http.Header)
	hop := make(map[string]bool)
	for k, values := range headers {
		if strings.EqualFold(k, "Connection") {
			for _, value := range values {
				for _, token := range strings.Split(value, ",") {
					hop[strings.ToLower(strings.TrimSpace(token))] = true
				}
			}
		}
	}
	for k, v := range headers {
		if hop[strings.ToLower(k)] {
			continue
		}
		switch strings.ToLower(k) {
		case "host", "origin", "cookie", "set-cookie", "connection", "upgrade", "content-length", "transfer-encoding", "proxy-authorization", "proxy-authenticate", "proxy-connection", "trailer", "te", "keep-alive", "accept-encoding", "referer":
			continue
		}
		if strings.HasPrefix(strings.ToLower(k), "sec-") {
			continue
		}
		result[k] = v
	}
	return result
}

type requestBody struct {
	ch      ssh.Channel
	pending []byte
	total   int64
	done    bool
}

func (r *requestBody) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, io.EOF
		}
		kind, data, err := ReadRecord(r.ch)
		if err != nil {
			return 0, err
		}
		switch kind {
		case RecordEnd:
			r.done = true
			return 0, io.EOF
		case RecordData:
			r.total += int64(len(data))
			if r.total > MaxBody {
				return 0, errors.New("request body exceeds limit")
			}
			r.pending = data
		default:
			return 0, errors.New("invalid request body record")
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func (*requestBody) Close() error { return nil }
func (h *Host) serveHTTP(parent context.Context, ch ssh.Channel, r BrowserRequest) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()
	method := r.Method
	if method == "" {
		method = "GET"
	}
	if method == "CONNECT" || method == "TRACE" {
		return errors.New("unsupported HTTP method")
	}
	body := &requestBody{ch: ch}
	req, err := http.NewRequestWithContext(ctx, method, h.requestURL(r.Path), body)
	if err != nil {
		return err
	}
	req.Header = cleanHeaders(r.Headers)
	req.Header.Set("Origin", "http://"+req.URL.Host)
	req.Header.Set("Accept-Encoding", "identity")
	client := h.httpClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 && response.Header.Get("Location") != "" {
		location, err := response.Location()
		if err != nil {
			return err
		}
		if location.Scheme != "http" || location.Host != req.URL.Host {
			return errors.New("redirect leaves the shared service")
		}
		response.Header.Set("Location", location.RequestURI())
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return errors.New("unsupported preview content encoding")
	}
	headers := cleanHeaders(response.Header)
	headers.Del("Content-Encoding")
	metadata, err := json.Marshal(BrowserResponse{Status: response.StatusCode, Headers: headers})
	if err != nil {
		return err
	}
	if err = WriteRecord(ch, RecordMetadata, metadata); err != nil {
		return err
	}
	buffer := make([]byte, MaxRecord)
	var total int64
	for {
		n, readErr := response.Body.Read(buffer)
		total += int64(n)
		if total > MaxBody {
			return errors.New("response exceeds preview limit")
		}
		if n > 0 {
			if err = WriteRecord(ch, RecordData, buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return WriteRecord(ch, RecordEnd, nil)
}
func (h *Host) serveWebSocket(parent context.Context, ch ssh.Channel, r BrowserRequest) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	client := h.httpClient()
	defer client.CloseIdleConnections()
	header := cleanHeaders(r.Headers)
	header.Set("Origin", "http://localhost:"+strconv.Itoa(h.config.Port))
	handshake, cancelHandshake := context.WithTimeout(ctx, authTimeout)
	socket, _, err := websocket.Dial(handshake, strings.Replace(h.requestURL(r.Path), "http:", "ws:", 1), &websocket.DialOptions{HTTPClient: client, HTTPHeader: header, Subprotocols: r.Protocols})
	cancelHandshake()
	if err != nil {
		return err
	}
	defer socket.CloseNow()
	socket.SetReadLimit(MaxWebSocketMessage)
	metadata, _ := json.Marshal(BrowserResponse{Protocol: socket.Subprotocol()})
	if err = WriteRecord(ch, RecordMetadata, metadata); err != nil {
		return err
	}
	writeDone := make(chan error, 1)
	go func() {
		for {
			kind, data, e := ReadRecord(ch)
			if e != nil {
				writeDone <- e
				cancel()
				return
			}
			switch kind {
			case RecordText, RecordBinary:
				messageType := websocket.MessageText
				if kind == RecordBinary {
					messageType = websocket.MessageBinary
				}
				e = socket.Write(ctx, messageType, data)
			case RecordClose:
				var closeInfo struct {
					Code   int    `json:"code"`
					Reason string `json:"reason"`
				}
				e = json.Unmarshal(data, &closeInfo)
				if e == nil {
					if closeInfo.Code != 1000 && (closeInfo.Code < 3000 || closeInfo.Code > 4999) {
						closeInfo.Code = 1000
					}
					e = socket.Close(websocket.StatusCode(closeInfo.Code), closeInfo.Reason)
				}
				writeDone <- e
				cancel()
				return
			default:
				e = fmt.Errorf("invalid WebSocket record")
			}
			if e != nil {
				writeDone <- e
				cancel()
				return
			}
		}
	}()
	defer func() { _ = ch.Close(); cancel(); <-writeDone }()
	for {
		kind, data, err := socket.Read(ctx)
		if err != nil {
			status := int(websocket.CloseStatus(err))
			if status < 0 {
				status = 1006
			}
			record, _ := json.Marshal(map[string]any{"code": status, "reason": ""})
			return WriteRecord(ch, RecordClose, record)
		}
		recordType := RecordText
		if kind == websocket.MessageBinary {
			recordType = RecordBinary
		}
		if err = WriteRecord(ch, recordType, data); err != nil {
			return err
		}
	}
}
