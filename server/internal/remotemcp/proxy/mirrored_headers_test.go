package proxy_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const mirroredToolsCallBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`

func postJSONWithHeaders(t *testing.T, p interface {
	Post(http.ResponseWriter, *http.Request) error
}, body string, headers http.Header,
) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	rr := httptest.NewRecorder()
	if err := p.Post(rr, req); err != nil {
		return rr, fmt.Errorf("proxy post: %w", err)
	}
	return rr, nil
}

func mirroredEchoingUpstream(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func TestProxy_Post_RejectsMcpNameContradictingBody(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	p := newProxyForTest(t, mirroredEchoingUpstream(t, &hits).URL)

	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, http.Header{
		"Mcp-Method": {"tools/call"},
		"Mcp-Name":   {"withheld"},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), hits.Load())
	require.Equal(t, http.StatusOK, rr.Code, "JSON-RPC requests carry protocol errors in a successful HTTP response")
	require.Contains(t, rr.Body.String(), "header mismatch")
}

func TestProxy_Post_ForwardsMatchingMirroredHeaders(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	p := newProxyForTest(t, mirroredEchoingUpstream(t, &hits).URL)

	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, http.Header{
		"Mcp-Method": {"tools/call"},
		"Mcp-Name":   {"allowed"},
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), hits.Load())
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestProxy_Post_RevalidatesMirroredHeadersAfterInterceptorMutation(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	p := newProxyForTest(t, mirroredEchoingUpstream(t, &hits).URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{
			name: "rewrite-mirrored-name",
			mutator: func(req *proxy.UserRequest) {
				req.UserHTTPRequest.Header.Set("Mcp-Name", "withheld")
			},
		},
	}

	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, http.Header{"Mcp-Name": {"allowed"}})
	require.NoError(t, err)
	require.Equal(t, int32(0), hits.Load())
	require.Contains(t, rr.Body.String(), "header mismatch")
}

func TestProxy_Post_RevalidatesConfiguredMirroredHeaders(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	p := newProxyForTest(t, mirroredEchoingUpstream(t, &hits).URL)
	p.Headers = []proxy.ConfiguredHeader{{Name: "Mcp-Name", StaticValue: "withheld"}}

	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, http.Header{"Mcp-Name": {"allowed"}})
	require.NoError(t, err)
	require.Equal(t, int32(0), hits.Load())
	require.Contains(t, rr.Body.String(), "header mismatch")
}

// TestProxy_Post_ForwardsStandardRequestHeadersUnmodified pins the
// intermediary obligation in MCP 2026-07-28: the standard request headers,
// including an Mcp-Param-{Name} header the proxy has no annotation for,
// reach the upstream exactly as the client sent them.
func TestProxy_Post_ForwardsStandardRequestHeadersUnmodified(t *testing.T) {
	t.Parallel()
	received := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)
	p := newProxyForTest(t, upstream.URL)

	sent := http.Header{
		"Mcp-Method":       {"tools/call"},
		"Mcp-Name":         {"allowed"},
		"Mcp-Param-Region": {"us-west1"},
		"Mcp-Param-Note":   {"=?base64?w6lsw6h2ZQ==?="},
	}
	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, sent)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Code)

	forwarded := <-received
	for name, values := range sent {
		require.Equal(t, values, forwarded.Values(name), "header %s", name)
	}
}

// TestProxy_Post_RejectsBase64EncodedMcpMethod pins that Mcp-Method is never
// Base64-decoded: MCP 2026-07-28 defines the sentinel only for Mcp-Name and
// Mcp-Param-{Name}, so a wrapped method is a literal that cannot match.
func TestProxy_Post_RejectsBase64EncodedMcpMethod(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	p := newProxyForTest(t, mirroredEchoingUpstream(t, &hits).URL)

	rr, err := postJSONWithHeaders(t, p, mirroredToolsCallBody, http.Header{
		"Mcp-Method": {"=?base64?dG9vbHMvY2FsbA==?="},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), hits.Load())
	require.Contains(t, rr.Body.String(), "header mismatch")
}
