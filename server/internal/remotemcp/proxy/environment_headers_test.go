package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// A cross-origin redirect (a different port, which net/http's own stripping
// ignores) loses the environment header in every spelling and the caller
// credential a replaced pass-through row would have read.
func TestProxy_Post_StripsEnvironmentHeadersOnCrossOriginRedirect(t *testing.T) {
	t.Parallel()

	redirectedHeaders := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedHeaders <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
	}))
	t.Cleanup(target.Close)

	originHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHeaders <- r.Header.Clone()
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.Identity.RemoteMCPServerID = "remote-under-test"
	p.Headers = []proxy.ConfiguredHeader{{
		Name: "X-Upstream-Key", StaticValue: "", ValueFromRequestHeader: "X-Caller-Key", IsRequired: true,
	}}
	p.EnvironmentHeaders = []proxy.ConfiguredHeader{{
		Name: "X-Upstream-Key", StaticValue: "synthetic-env-key", ValueFromRequestHeader: "", IsRequired: true,
	}, {
		Name: "X-Instance-Url", StaticValue: "synthetic-env-instance", ValueFromRequestHeader: "", IsRequired: true,
	}}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Caller-Key", "synthetic-client-key")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)

	origin := <-originHeaders
	require.Equal(t, "synthetic-env-key", origin.Get("X-Upstream-Key"))
	require.Equal(t, "synthetic-env-instance", origin.Get("X-Instance-Url"))
	// The caller's credential reaches the configured origin, so its absence
	// after the redirect proves it was stripped.
	require.Equal(t, "synthetic-client-key", origin.Get("X-Caller-Key"))

	redirected := <-redirectedHeaders
	for name, values := range redirected {
		for _, v := range values {
			require.NotContains(t, []string{"synthetic-env-key", "synthetic-env-instance", "synthetic-client-key"}, v, "header %s leaked across origins", name)
		}
	}
}
