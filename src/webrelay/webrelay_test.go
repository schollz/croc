package webrelay

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSite() fstest.MapFS {
	return fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><div id=\"root\"></div>")},
		"assets/app.js":  {Data: []byte("console.log('croc')")},
		"assets/app.css": {Data: []byte("body { color: black; }")},
	}
}

func startEchoServer(t *testing.T) (host, port string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()

	host, port, err = net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	return
}

func TestNormalizeConfigAllowsPublicRelayPortPools(t *testing.T) {
	config, err := normalizeConfig(Config{})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]string{"9009", "9010", "9011", "9012", "9013", "9014", "9015", "9016", "9017"},
		config.AllowedPorts,
	)
}

func TestHealthAndMethod(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "ok\n", recorder.Body.String())

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestServesSiteAndRuntimeConfig(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:     "relay.example.test",
		AllowedPorts:  []string{"9109", "9110"},
		RelayPassword: "relay-secret",
		StaticFiles:   testSite(),
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `id="root"`)
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/send/files", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `id="root"`)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing.js", nil))
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/config.js", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.JSONEq(
		t,
		`{"gatewayURL":"/ws","relayAddress":"relay.example.test:9109","relayPassword":"relay-secret"}`,
		strings.TrimSuffix(
			strings.TrimPrefix(recorder.Body.String(), "window.__CROC_RUNTIME_CONFIG__ = "),
			";\n",
		),
	)
}

func TestRejectsPortOutsideAllowlist(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ws?port=22", nil))
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestWebSocketForwardsBinaryStream(t *testing.T) {
	host, port := startEchoServer(t)
	handler, err := Handler(Config{
		RelayHost:      host,
		AllowedPorts:   []string{port},
		OriginPatterns: []string{"example.test"},
		StaticFiles:    testSite(),
	})
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?port=" + port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://example.test"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.CloseNow() })

	payload := []byte("croc\x05\x00\x00\x00hello")
	require.NoError(t, connection.Write(ctx, websocket.MessageBinary, payload))
	messageType, received, err := connection.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageBinary, messageType)
	assert.Equal(t, payload, received)
}

func TestRejectsUnexpectedOrigin(t *testing.T) {
	host, port := startEchoServer(t)
	handler, err := Handler(Config{
		RelayHost:      host,
		AllowedPorts:   []string{port},
		OriginPatterns: []string{"allowed.test"},
		StaticFiles:    testSite(),
	})
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?port=" + port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://denied.test"}},
	})
	if connection != nil {
		_ = connection.CloseNow()
	}
	require.Error(t, err)
	require.NotNil(t, response)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
}
