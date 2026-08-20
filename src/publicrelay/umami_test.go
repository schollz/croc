package publicrelay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUmamiReporterTracksExplicitAndDefaultPaths(t *testing.T) {
	type receivedRequest struct {
		request     umamiRequest
		path        string
		contentType string
		err         error
	}
	requests := make(chan receivedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request umamiRequest
		decodeErr := json.NewDecoder(r.Body).Decode(&request)
		requests <- receivedRequest{
			request:     request,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			err:         decodeErr,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	reporter, err := NewUmamiReporter(
		server.URL,
		"website-uuid",
		"https://getcroc.com",
		"11.2.2",
	)
	require.NoError(t, err)
	defer reporter.Close()

	reporter.TrackPath("installer-curl", "/")
	reporter.Track("relay-session")

	want := []struct {
		name string
		path string
	}{
		{name: "installer-curl", path: "/"},
		{name: "relay-session", path: "/relay"},
	}
	for _, expected := range want {
		select {
		case received := <-requests:
			require.NoError(t, received.err)
			assert.Equal(t, "/api/send", received.path)
			assert.Equal(t, "application/json", received.contentType)
			assert.Equal(t, "event", received.request.Type)
			assert.Equal(t, "getcroc.com", received.request.Payload.Hostname)
			assert.Equal(t, "website-uuid", received.request.Payload.Website)
			assert.Equal(t, expected.name, received.request.Payload.Name)
			assert.Equal(t, expected.path, received.request.Payload.URL)
			assert.Equal(t, map[string]string{"version": "11.2.2"}, received.request.Payload.Data)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s event", expected.name)
		}
	}
}

func TestUmamiReporterRejectsUnsafeEventPaths(t *testing.T) {
	reporter := &UmamiReporter{
		events: make(chan umamiEvent, 1),
		ctx:    t.Context(),
	}

	for _, eventPath := range []string{"", "relay", "//example.com/relay", "/?code=secret", "/#secret"} {
		reporter.TrackPath("event", eventPath)
	}
	assert.Empty(t, reporter.events)
}
