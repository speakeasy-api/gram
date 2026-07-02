package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// upstream401 serves an upstream that rejects every request with a 401 and
// its own RFC 6750 challenge naming its own protected-resource metadata.
func upstream401(t *testing.T) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://upstream.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token","error_description":"Invalid access token"}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func TestProxy_Post_Upstream401_ReplacesWWWAuthenticateWhenSet(t *testing.T) {
	t.Parallel()

	upstream := upstream401(t)
	p := newProxyForTest(t, upstream.URL)
	p.WWWAuthenticate = `Bearer resource_metadata="https://gram.example.com/.well-known/oauth-protected-resource/x/mcp/slug"`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t,
		`Bearer resource_metadata="https://gram.example.com/.well-known/oauth-protected-resource/x/mcp/slug"`,
		rr.Header().Get("WWW-Authenticate"),
		"relayed 401 must challenge the client with this server's resource metadata, not the upstream's")
	require.Contains(t, rr.Body.String(), "invalid_token", "upstream body still relays")
}

func TestProxy_Post_Upstream401_RelaysWWWAuthenticateWhenUnset(t *testing.T) {
	t.Parallel()

	upstream := upstream401(t)
	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Header().Get("WWW-Authenticate"), "upstream.example.com",
		"without a challenge of our own (external OAuth passthrough) the upstream challenge relays verbatim")
}

func TestProxy_Post_Upstream200_DoesNotInjectHeader(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.WWWAuthenticate = `Bearer resource_metadata="https://gram.example.com/meta"`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Equal(t, http.StatusOK, rr.Code)
	require.Empty(t, rr.Header().Get("WWW-Authenticate"), "successful responses carry no challenge")
}
