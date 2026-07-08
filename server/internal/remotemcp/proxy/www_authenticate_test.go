package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// upstreamRejecting serves an upstream that rejects every request with the
// given status and its own RFC 6750 challenge naming its own
// protected-resource metadata.
func upstreamRejecting(t *testing.T, status int) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://upstream.example.com/.well-known/oauth-protected-resource/mcp", error="invalid_token"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"invalid_token","error_description":"Invalid access token"}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func TestProxy_Post_Upstream401_ReplacesWWWAuthenticateWhenSet(t *testing.T) {
	t.Parallel()

	upstream := upstreamRejecting(t, http.StatusUnauthorized)
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

func TestProxy_Post_Upstream403_ReplacesWWWAuthenticateWhenSet(t *testing.T) {
	t.Parallel()

	upstream := upstreamRejecting(t, http.StatusForbidden)
	p := newProxyForTest(t, upstream.URL)
	p.WWWAuthenticate = `Bearer resource_metadata="https://gram.example.com/meta"`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Equal(t, http.StatusForbidden, rr.Code)
	require.Equal(t, `Bearer resource_metadata="https://gram.example.com/meta"`,
		rr.Header().Get("WWW-Authenticate"), "403 substitutes the challenge the same as 401")
}

func TestProxy_Get_UpstreamSSE401_ReplacesWWWAuthenticateWhenSet(t *testing.T) {
	t.Parallel()

	// A 401 carrying Content-Type: text/event-stream routes through the SSE
	// relay rather than writeResponse; the challenge must substitute there too.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://upstream.example.com/.well-known/oauth-protected-resource/mcp"`)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.WWWAuthenticate = `Bearer resource_metadata="https://gram.example.com/meta"`

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, `Bearer resource_metadata="https://gram.example.com/meta"`,
		rr.Header().Get("WWW-Authenticate"), "SSE relay path substitutes the challenge too")
}

func TestProxy_Post_Upstream401_RelaysWWWAuthenticateWhenUnset(t *testing.T) {
	t.Parallel()

	upstream := upstreamRejecting(t, http.StatusUnauthorized)
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
