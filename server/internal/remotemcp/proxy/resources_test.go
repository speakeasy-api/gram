package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const (
	resourcesReadRequest    = `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"file:///etc/hosts"}}`
	resourcesReadResponse   = `{"jsonrpc":"2.0","id":3,"result":{"contents":[{"uri":"file:///etc/hosts","text":"127.0.0.1 localhost"}]}}`
	resourcesListRequest    = `{"jsonrpc":"2.0","id":4,"method":"resources/list","params":{}}`
	resourcesListThreeItems = `{
        "jsonrpc":"2.0",
        "id":4,
        "result":{
            "resources":[
                {"name":"a","uri":"file:///a"},
                {"name":"b","uri":"file:///b"},
                {"name":"c","uri":"file:///c"}
            ]
        }
    }`
)

func TestProxy_Post_ResourcesReadRequestInterceptor_RunsForResourcesRead(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resourcesReadResponse))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ResourcesReadRequest
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesReadRequestInterceptors = []proxy.ResourcesReadRequestInterceptor{
		&mockResourcesReadRequestInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesReadRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed request interceptor must be invoked for resources/read")
	require.NotNil(t, observed.Params)
	require.Equal(t, "file:///etc/hosts", observed.Params.URI)
}

func TestProxy_Post_ResourcesReadRequestInterceptor_SkipsForNonResourcesRead(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	var called int
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesReadRequestInterceptors = []proxy.ResourcesReadRequestInterceptor{
		&mockResourcesReadRequestInterceptor{name: "typed", called: &called},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Zero(t, called, "typed resources/read interceptor must not run for initialize")
}

func TestProxy_Post_ResourcesReadRequestInterceptor_RejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when a typed interceptor rejects resources/read")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ResourcesReadRequestInterceptors = []proxy.ResourcesReadRequestInterceptor{
		&mockResourcesReadRequestInterceptor{name: "typed", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "policy violation", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesReadRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"id":3`, "envelope must preserve the originating resources/read request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate to the envelope")
	require.Contains(t, rr.Body.String(), "policy violation")
}

func TestProxy_Post_ResourcesReadResponseInterceptor_RunsForSuccessResult(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resourcesReadResponse))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ResourcesReadResponse
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesReadResponseInterceptors = []proxy.ResourcesReadResponseInterceptor{
		&mockResourcesReadResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesReadRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed response interceptor must run on resources/read response")
	require.NotNil(t, observed.Result)
	require.Len(t, observed.Result.Contents, 1)
	require.Equal(t, "file:///etc/hosts", observed.Result.Contents[0].URI)
}

func TestProxy_Post_ResourcesListRequestInterceptor_RunsForResourcesList(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resourcesListThreeItems))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ResourcesListRequest
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesListRequestInterceptors = []proxy.ResourcesListRequestInterceptor{
		&mockResourcesListRequestInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed request interceptor must be invoked for resources/list")
}

func TestProxy_Post_ResourcesListResponseInterceptor_RunsForSuccessResult(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resourcesListThreeItems))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ResourcesListResponse
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesListResponseInterceptors = []proxy.ResourcesListResponseInterceptor{
		&mockResourcesListResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed)
	require.NotNil(t, observed.Result)
	require.Len(t, observed.Result.Resources, 3)
}

func TestProxy_Post_ResourcesListResponse_SetResources_RewritesRelayedBody(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resourcesListThreeItems)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ResourcesListResponseInterceptors = []proxy.ResourcesListResponseInterceptor{
		&mutatingResourcesListResponseInterceptor{
			name: "keep-only-a",
			resourcesFn: func(resources []*mcp.Resource) []*mcp.Resource {
				kept := resources[:0]
				for _, r := range resources {
					if r.URI == "file:///a" {
						kept = append(kept, r)
					}
				}
				return kept
			},
		},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	out := rr.Body.String()
	require.Contains(t, out, `"file:///a"`, "kept resource must reach the client")
	require.NotContains(t, out, `"file:///b"`, "filtered resource must not reach the client")
	require.NotContains(t, out, `"file:///c"`, "filtered resource must not reach the client")
	require.Contains(t, out, `"id":4`, "response id must survive re-encoding")
}

func TestProxy_Post_ResourcesListResponse_SetResources_EmptyArrayWhenAllFiltered(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resourcesListThreeItems)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ResourcesListResponseInterceptors = []proxy.ResourcesListResponseInterceptor{
		&mutatingResourcesListResponseInterceptor{
			name: "drop-all",
			resourcesFn: func(_ []*mcp.Resource) []*mcp.Resource {
				return nil
			},
		},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	out := rr.Body.String()
	require.Contains(t, out, `"resources":[]`, "nil resources must serialize as empty array, not null")
}

func TestProxy_Post_SSEResponse_TerminalEventDispatchesTypedResourcesReadInterceptor(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		resourcesReadResponse,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observedTerminal *proxy.ResourcesReadResponse
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesReadResponseInterceptors = []proxy.ResourcesReadResponseInterceptor{
		&mockResourcesReadResponseInterceptor{name: "typed", lastCall: &observedTerminal},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesReadRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observedTerminal, "typed resources/read response interceptor must fire on terminal SSE event")
	require.NotNil(t, observedTerminal.Result)
	require.Len(t, observedTerminal.Result.Contents, 1)
}

func TestProxy_Post_SSEResponse_TerminalEventDispatchesTypedResourcesListInterceptor(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		strings.ReplaceAll(resourcesListThreeItems, "\n", ""),
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observedTerminal *proxy.ResourcesListResponse
	p := newProxyForTest(t, upstream.URL)
	p.ResourcesListResponseInterceptors = []proxy.ResourcesListResponseInterceptor{
		&mockResourcesListResponseInterceptor{name: "typed", lastCall: &observedTerminal},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(resourcesListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observedTerminal, "typed resources/list response interceptor must fire on terminal SSE event")
	require.NotNil(t, observedTerminal.Result)
	require.Len(t, observedTerminal.Result.Resources, 3)
}
