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
	"github.com/schollz/croc/v11/src/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testInstaller = "#!/bin/bash\nset -o nounset\n"

func testSite() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                        {Data: []byte("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>")},
		"blog/index.html":                   {Data: []byte("<!doctype html><html><head><title>field notes</title></head><body><div id=\"root\"></div></body></html>")},
		"blog/pake-step-by-step/index.html": {Data: []byte("<!doctype html><html><head><title>PAKE, step by step</title></head><body><div id=\"root\"></div></body></html>")},
		"default.txt":                       {Data: []byte(testInstaller)},
		"croc-download-sw.js": {
			Data: []byte("self.addEventListener('fetch', () => {})"),
		},
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
	assert.Equal(t, []string{"1.getcroc.com", "2.getcroc.com", "3.getcroc.com"}, config.RelayHosts)
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
	assert.NotContains(t, recorder.Body.String(), "field notes")

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/blog", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "<title>field notes</title>")
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/blog/pake-step-by-step", nil),
	)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "<title>PAKE, step by step</title>")
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/croc-download-sw.js", nil),
	)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing.js", nil))
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/config.js", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.JSONEq(
		t,
		`{"gatewayURL":"/ws","relayAddresses":["relay.example.test:9109"],"relayPassword":"relay-secret","store":{"enabled":false,"maxTransferBytes":0,"maxFiles":0,"maxDownloads":0,"expiresSeconds":0,"maxExpiresSeconds":0}}`,
		strings.TrimSuffix(
			strings.TrimPrefix(recorder.Body.String(), "window.__CROC_RUNTIME_CONFIG__ = "),
			";\n",
		),
	)
}

func TestUmamiScriptRequiresBothEnvironmentValues(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		url        string
		websiteID  string
		configured bool
	}{
		{name: "unset"},
		{name: "URL only", url: "https://umami.schollz.com"},
		{name: "website ID only", websiteID: "website-uuid"},
		{
			name:       "both values",
			url:        "https://umami.schollz.com/",
			websiteID:  "website-uuid",
			configured: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler, err := Handler(Config{
				RelayHost:      "127.0.0.1",
				AllowedPorts:   []string{"9009"},
				StaticFiles:    testSite(),
				UmamiURL:       testCase.url,
				UmamiWebsiteID: testCase.websiteID,
			})
			require.NoError(t, err)

			script := `<script defer src="https://umami.schollz.com/script.js" data-website-id="website-uuid" data-performance="true"></script>`
			for _, requestPath := range []string{"/", "/blog"} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(
					recorder,
					httptest.NewRequest(http.MethodGet, requestPath, nil),
				)

				assert.Equal(
					t,
					testCase.configured,
					strings.Contains(recorder.Body.String(), script),
				)
			}
		})
	}
}

func TestGoogleAdSenseConfig(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		publisherID string
		wantScript  string
	}{
		{name: "unset"},
		{
			name:        "configured",
			publisherID: "ca-pub-4947875154879707",
			wantScript:  `<script async src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=ca-pub-4947875154879707" crossorigin="anonymous"></script>`,
		},
		{
			name:        "escapes publisher ID",
			publisherID: `publisher&amp;client`,
			wantScript:  `client=publisher%26amp%3Bclient`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler, err := Handler(Config{
				RelayHost:     "127.0.0.1",
				AllowedPorts:  []string{"9009"},
				StaticFiles:   testSite(),
				GoogleAdSense: testCase.publisherID,
			})
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, "/", nil),
			)

			if testCase.wantScript == "" {
				assert.NotContains(t, recorder.Body.String(), "pagead2.googlesyndication.com")
			} else {
				assert.Contains(t, recorder.Body.String(), testCase.wantScript)
			}
		})
	}
}

func TestGoogleAdsTXT(t *testing.T) {
	const contents = "google.com, pub-4947875154879707, DIRECT, f08c47fec0942fa0\n"
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
		GoogleAdsTXT: contents,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ads.txt", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, contents, recorder.Body.String())
	assert.Equal(t, "text/plain; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/ads.txt", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Body.String())
	assert.Equal(t, int64(len(contents)), recorder.Result().ContentLength)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ads.txt", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestGoogleAdsTXTIsAbsentWhenUnconfigured(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ads.txt", nil))
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestEnablesStoredTransferRuntimeAndAPI(t *testing.T) {
	storage, err := store.New(store.Config{
		Root:             t.TempDir(),
		MaxTransferBytes: 8 << 20,
		MaxTotalBytes:    32 << 20,
		MinFreeBytes:     1,
		MaxFiles:         7,
		MaxDownloads:     9,
		MaxExpiration:    3 * 24 * time.Hour,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	handler, err := Handler(Config{
		RelayHost:    "relay.example.test",
		AllowedPorts: []string{"9109"},
		StaticFiles:  testSite(),
		StoreService: storage,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/config.js", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"enabled":true`)
	assert.Contains(t, recorder.Body.String(), `"maxTransferBytes":8388608`)
	assert.Contains(t, recorder.Body.String(), `"maxFiles":7`)
	assert.Contains(t, recorder.Body.String(), `"maxDownloads":9`)
	assert.Contains(t, recorder.Body.String(), `"expiresSeconds":86400`)
	assert.Contains(t, recorder.Body.String(), `"maxExpiresSeconds":259200`)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/store/transfers", nil),
	)
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestRootNegotiatesInstallerForCommandLineClients(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)

	for _, userAgent := range []string{
		"curl/8.10.1",
		"Wget/1.24.5",
	} {
		t.Run(userAgent, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("User-Agent", userAgent)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, testInstaller, recorder.Body.String())
			assert.Equal(t, "text/plain; charset=utf-8", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			assert.Equal(t, "User-Agent", recorder.Header().Get("Vary"))
			assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
		})
	}

	request := httptest.NewRequest(http.MethodHead, "/", nil)
	request.Header.Set("User-Agent", "curl/8.10.1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Body.String())
	assert.Equal(t, int64(len(testInstaller)), recorder.Result().ContentLength)
}

func TestRootContinuesServingSiteToBrowsers(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 Firefox/142.0")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `id="root"`)
	assert.Equal(t, "User-Agent", recorder.Header().Get("Vary"))

	request = httptest.NewRequest(http.MethodGet, "/send/files", nil)
	request.Header.Set("User-Agent", "curl/8.10.1")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `id="root"`)
}

func TestRejectsPortOutsideAllowlist(t *testing.T) {
	handler, err := Handler(Config{
		RelayHost:    "127.0.0.1",
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ws?relay=0&port=22", nil))
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestRejectsMissingAndOutOfRangeRelayIndex(t *testing.T) {
	handler, err := Handler(Config{
		RelayHosts:   []string{"127.0.0.1", "127.0.0.2"},
		AllowedPorts: []string{"9009"},
		StaticFiles:  testSite(),
	})
	require.NoError(t, err)
	for _, target := range []string{"/ws?port=9009", "/ws?relay=2&port=9009", "/ws?relay=nope&port=9009"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusForbidden, recorder.Code)
	}
}

func TestWebSocketRoutesByRelayIndex(t *testing.T) {
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	_, port, err := net.SplitHostPort(first.Addr().String())
	require.NoError(t, err)
	second, err := net.Listen("tcp6", net.JoinHostPort("::1", port))
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	serveMarker := func(listener net.Listener, marker string) {
		go func() {
			for {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				go func() {
					defer connection.Close()
					buffer := make([]byte, 64)
					if _, readErr := connection.Read(buffer); readErr == nil {
						_, _ = connection.Write([]byte(marker))
					}
				}()
			}
		}()
	}
	serveMarker(first, "first")
	serveMarker(second, "second")

	handler, err := Handler(Config{
		RelayHosts:     []string{"127.0.0.1", "::1"},
		AllowedPorts:   []string{port},
		OriginPatterns: []string{"example.test"},
		StaticFiles:    testSite(),
	})
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?relay=1&port=" + port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://example.test"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.CloseNow() })
	require.NoError(t, connection.Write(ctx, websocket.MessageBinary, []byte("request")))
	_, response, err := connection.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "second", string(response))
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

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?relay=0&port=" + port
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

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?relay=0&port=" + port
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
