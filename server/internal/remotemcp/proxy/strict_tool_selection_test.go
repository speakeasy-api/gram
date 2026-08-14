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
)

const (
	strictToolsCallMalformed = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":"not-an-object"}`
	strictToolsListRequest   = `{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{}}`
)

func postJSON(t *testing.T, p interface {
	Post(http.ResponseWriter, *http.Request) error
}, body string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	if err := p.Post(rr, req); err != nil {
		return rr, fmt.Errorf("proxy post: %w", err)
	}
	return rr, nil
}

func TestProxy_Post_StrictRejectsMalformedToolsCallBeforeUpstream(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsCallMalformed)
	require.NoError(t, err)
	require.Equal(t, int32(0), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "-32602")
	require.Contains(t, rr.Body.String(), "malformed tools/call request")
}

func TestProxy_Post_StrictRejectsMalformedToolsListBeforeUpstream(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":8,"method":"tools/list","params":"not-an-object"}`)
	require.NoError(t, err)
	require.Equal(t, int32(0), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "-32602")
	require.Contains(t, rr.Body.String(), "malformed tools/list request")
}

func TestProxy_Post_NonStrictForwardsMalformedToolsList(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":8,"error":{"code":-32602,"message":"upstream says no"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":8,"method":"tools/list","params":"not-an-object"}`)
	require.NoError(t, err)
	require.Equal(t, int32(1), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "upstream says no")
}

func TestProxy_Post_StrictRejectsDuplicateJSONMembersBeforeUpstream(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	// Go's last-wins decoder reads method "ping"; a first-wins upstream
	// parser would execute tools/call. Strict sessions must never forward it.
	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":3,"method":"tools/call","method":"ping"}`)
	require.NoError(t, err)
	require.Equal(t, int32(0), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "-32600")
	require.Contains(t, rr.Body.String(), `duplicate object member \"method\"`)
}

func TestProxy_Post_StrictRejectsDuplicateToolNameBeforeUpstream(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"allowed","name":"forbidden","arguments":{}}}`)
	require.NoError(t, err)
	require.Equal(t, int32(0), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), `duplicate object member \"name\"`)
}

func TestProxy_Post_StrictRejectsEmptyBody(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, "")
	require.NoError(t, err)
	require.Equal(t, int32(0), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "exactly one JSON-RPC message is required")
}

func TestProxy_Post_NonStrictForwardsDuplicateJSONMembers(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	_, err := postJSON(t, p, `{"jsonrpc":"2.0","id":3,"method":"ping","method":"ping"}`)
	require.NoError(t, err)
	require.Equal(t, int32(1), upstreamHits.Load())
}

func TestProxy_Post_NonStrictForwardsMalformedToolsCall(t *testing.T) {
	t.Parallel()

	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"upstream says no"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	rr, err := postJSON(t, p, strictToolsCallMalformed)
	require.NoError(t, err)
	require.Equal(t, int32(1), upstreamHits.Load())
	require.Contains(t, rr.Body.String(), "upstream says no")
}

func TestProxy_Post_StrictFailsClosedOnUndecodableToolsListResult(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":9,"result":{"tools":"not-an-array"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.NotContains(t, rr.Body.String(), "not-an-array")
	require.Contains(t, rr.Body.String(), "unreadable tools/list response")
}

func TestProxy_Post_NonStrictRelaysUndecodableToolsListResultVerbatim(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":9,"result":{"tools":"not-an-array"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.Contains(t, rr.Body.String(), "not-an-array")
}

func TestProxy_Post_StrictPassesThroughValidToolsListError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":9,"error":{"code":-32603,"message":"upstream exploded"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.Contains(t, rr.Body.String(), "upstream exploded")
}

func TestProxy_Post_StrictFailsClosedOnEmptyToolsListBody(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.Contains(t, rr.Body.String(), "unreadable tools/list response")
}

func TestProxy_Post_StrictRelaysNon2xxToolsListVerbatim(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://upstream.example/.well-known"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`upstream challenge`)) //nolint:misspell // literal body
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "upstream challenge")
}

func TestProxy_Post_StrictSubstitutesUnreadableSSETerminalToolsList(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":{\"tools\":\"not-an-array\"}}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.NotContains(t, rr.Body.String(), "not-an-array")
	require.Contains(t, rr.Body.String(), "unreadable tools/list response")
}

func TestProxy_Post_StrictRelaysNon2xxSSEDataEventsVerbatim(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("data: upstream challenge\n\n")) //nolint:misspell // literal body
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "upstream challenge")
}

func TestProxy_Post_StrictDropsUndecodableSSEDataEventOnToolsList(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: this is not json\n\n"))
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":{\"tools\":[]}}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.NotContains(t, rr.Body.String(), "this is not json")
	require.Contains(t, rr.Body.String(), `"tools":[]`)
}
