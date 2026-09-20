package storeclient

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/stretchr/testify/require"
)

func setTestProxies(t *testing.T, socks5, httpProxy string) {
	t.Helper()
	oldSOCKS, oldHTTP := comm.Socks5Proxy, comm.HttpProxy
	comm.Socks5Proxy, comm.HttpProxy = socks5, httpProxy
	t.Cleanup(func() { comm.Socks5Proxy, comm.HttpProxy = oldSOCKS, oldHTTP })
}

func TestHTTPClientProxySelection(t *testing.T) {
	for _, tt := range []struct {
		name, socks5, httpProxy, want string
		httpFromEnv                   bool
	}{
		{name: "SOCKS5", socks5: "socks.test:1080", want: "socks5://socks.test:1080"},
		{name: "HTTP", httpProxy: "http.test:8080", want: "http://http.test:8080"},
		{name: "both", socks5: "socks.test:1080", httpProxy: "http.test:8080", want: "socks5://socks.test:1080"},
		{name: "explicit schemes", socks5: "socks5h://socks.test:1080", httpProxy: "http://http.test:8080", want: "socks5h://socks.test:1080"},
		{name: "SOCKS5 with HTTP_PROXY", socks5: "socks.test:1080", httpProxy: "http://http.test:8080", httpFromEnv: true, want: "socks5://socks.test:1080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			httpProxy := tt.httpProxy
			if tt.httpFromEnv {
				t.Setenv("HTTP_PROXY", httpProxy)
				// The CLI populates comm.HttpProxy from --connect or HTTP_PROXY.
				httpProxy = os.Getenv("HTTP_PROXY")
			}
			setTestProxies(t, tt.socks5, httpProxy)
			client := new(Client).httpClient()
			t.Cleanup(client.CloseIdleConnections)
			request, err := http.NewRequest(http.MethodGet, "https://store.test", nil)
			require.NoError(t, err)
			selected, err := client.Transport.(*http.Transport).Proxy(request)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, tt.want, selected.String())
		})
	}
}

func TestHTTPClientReusesConnections(t *testing.T) {
	setTestProxies(t, "", "")
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "chunk")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	client := new(Client)
	t.Cleanup(func() { client.httpClient().CloseIdleConnections() })
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		response, err := client.do(request)
		require.NoError(t, err)
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		require.Equal(t, "chunk", string(body))
	}
	require.EqualValues(t, 1, connections.Load(), "sequential chunks should reuse one TCP connection")
}

func TestHTTPClientConcurrentRequests(t *testing.T) {
	setTestProxies(t, "", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := new(Client)
	t.Cleanup(func() { client.httpClient().CloseIdleConnections() })
	start := make(chan struct{})
	results := make(chan error, storedTransferWorkers)
	var workers sync.WaitGroup
	for range storedTransferWorkers {
		workers.Go(func() {
			<-start
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				results <- err
				return
			}
			response, err := client.do(request)
			if err == nil {
				err = response.Body.Close()
			}
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
}
