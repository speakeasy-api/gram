package proxy_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`

func newProxyForTest(t *testing.T, upstreamURL string) *proxy.Proxy {
	t.Helper()

	return &proxy.Proxy{
		HTTPClient:           http.DefaultClient,
		Logger:               discardLogger(),
		Tracer:               tracenoop.NewTracerProvider().Tracer("test"),
		NonStreamingTimeout:  5 * time.Second,
		StreamingTimeout:     5 * time.Second,
		MaxBufferedBodyBytes: proxy.DefaultMaxBufferedBodyBytes,
		RemoteURL:            upstreamURL,
	}
}

func TestProxy_Post_ForwardsRequestAndResponse(t *testing.T) {
	t.Parallel()

	var gotMethod, gotBody, gotContentType string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)
	require.NoError(t, err)

	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "application/json", gotContentType)
	require.JSONEq(t, initializeRequest, gotBody)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Contains(t, rr.Body.String(), `"protocolVersion":"2025-06-18"`)
}

func TestProxy_Post_StripsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer gram-api-key-should-not-leak")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Empty(t, gotAuth, "Gram API key must never be forwarded to the remote MCP server")
}

func TestProxy_Post_AppliesStaticHeader(t *testing.T) {
	t.Parallel()

	var gotAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-Remote-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.Headers = []proxy.ConfiguredHeader{
		{Name: "X-Remote-Api-Key", StaticValue: "upstream-secret", IsRequired: true},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, "upstream-secret", gotAPIKey)
}

func TestProxy_Post_PassThroughHeader(t *testing.T) {
	t.Parallel()

	var gotOwner string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOwner = r.Header.Get("X-Upstream-Owner")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.Headers = []proxy.ConfiguredHeader{
		{Name: "X-Upstream-Owner", ValueFromRequestHeader: "X-Gram-Owner"},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gram-Owner", "team-42")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, "team-42", gotOwner)
}

func TestProxy_Post_MissingRequiredPassThroughHeaderReturns400(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when required pass-through header is missing")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.Headers = []proxy.ConfiguredHeader{
		{Name: "X-Upstream-Owner", ValueFromRequestHeader: "X-Gram-Owner", IsRequired: true},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestProxy_Post_InvalidJSONRPCReturns400(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called for malformed JSON-RPC")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestProxy_Post_InterceptorsRunInOrder(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	order := []string{}
	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "req-a", order: &order},
		&mockUserRequestInterceptor{name: "req-b", order: &order},
	}
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "resp-a", order: &order},
		&mockRemoteMessageInterceptor{name: "resp-b", order: &order},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, []string{"req-a", "req-b", "resp-a", "resp-b"}, order)
}

func TestProxy_Post_UserRequestInterceptorRejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when an interceptor rejects the request")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "reject", err: errors.New("policy violation")},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "rejection writes a JSON-RPC error envelope rather than returning an error")
	require.Equal(t, http.StatusOK, rr.Code, "rejected requests with an id get HTTP 200 + JSON-RPC error body")
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Contains(t, rr.Body.String(), `"id":1`, "envelope must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"error"`, "envelope must carry the JSON-RPC error field")
	require.Contains(t, rr.Body.String(), `"code":-32603`, "default mapping for plain error rejections is RejectCodeInternalError")
}

func TestProxy_Post_TypedRejectErrorSurfacesThroughInterceptorWrap(t *testing.T) {
	t.Parallel()

	// Defensive: the run* helpers wrap interceptor errors via fmt.Errorf with
	// %w to preserve the chain. RejectErrorFromCause must walk that chain via
	// Unwrap and find the inner *RejectError so the typed code/message/data
	// flow through to the JSON-RPC envelope. Regression guard for the wrap's
	// interaction with typed rejections.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when an interceptor rejects")
	}))
	t.Cleanup(upstream.Close)

	typedReject := &proxy.RejectError{
		Code:    proxy.RejectCodeInvalidParams,
		Message: "missing required tool argument: location",
		Data:    map[string]any{"hint": "supply location"},
	}

	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "schema-check", err: typedReject},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)

	// The envelope must carry the TYPED code/message/data from the inner
	// *RejectError, not the generic mapping that would apply if the wrap
	// had masked the typed error.
	require.Contains(t, rr.Body.String(), `"code":-32602`, "typed RejectCodeInvalidParams must propagate through the interceptor wrap")
	require.Contains(t, rr.Body.String(), "missing required tool argument: location", "typed RejectError.Message must propagate")
	require.Contains(t, rr.Body.String(), `"hint":"supply location"`, "typed RejectError.Data must propagate")
}

func TestProxy_Post_OopsShareableErrorSurfacesThroughInterceptorWrap(t *testing.T) {
	t.Parallel()

	// Regression guard: an interceptor returning an *oops.ShareableError
	// (e.g. ToolUsageLimitsInterceptor returning oops.E(CodeForbidden, ...))
	// must surface the inner code through RejectErrorFromCause's
	// errors.AsType walk. Wrapping with oops.E in the run* helpers would
	// prepend an outer *oops.ShareableError with CodeUnexpected and shadow
	// the inner code — fmt.Errorf-based wrapping preserves the chain.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when an interceptor rejects")
	}))
	t.Cleanup(upstream.Close)

	innerOops := oops.E(oops.CodeForbidden, errors.New("tool usage limit reached"), "tool usage limit reached")

	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "policy-gate", err: innerOops},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)

	// CodeForbidden must map to RejectCodeInvalidRequest (-32600), matching
	// the existing /mcp endpoint's NewErrorFromCause table. -32603
	// (RejectCodeInternalError) would indicate the run* helpers replaced the
	// interceptor's code with CodeUnexpected before the chain walk.
	require.Contains(t, rr.Body.String(), `"code":-32600`, "inner CodeForbidden must propagate through the interceptor wrap")
	require.Contains(t, rr.Body.String(), "tool usage limit reached", "inner ShareableError.Error message must propagate")
}

// flushRecorder is a ResponseWriter wrapper that records each Write as a
// separate chunk and counts Flush calls, so tests can assert that the proxy is
// actually streaming events rather than buffering them until EOF.
type flushRecorder struct {
	mu         sync.Mutex
	header     http.Header
	statusCode int
	chunks     [][]byte
	flushes    int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header), statusCode: http.StatusOK}
}

func (f *flushRecorder) Header() http.Header { return f.header }

func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	buf := make([]byte, len(p))
	copy(buf, p)
	f.chunks = append(f.chunks, buf)
	f.mu.Unlock()
	return len(p), nil
}

func (f *flushRecorder) WriteHeader(code int) { f.statusCode = code }

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	f.flushes++
	f.mu.Unlock()
}

func (f *flushRecorder) flushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushes
}

func (f *flushRecorder) chunkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.chunks)
}

func TestProxy_Get_StreamsEventsIncrementally(t *testing.T) {
	t.Parallel()

	const events = 3

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "httptest server must expose a Flusher for this test", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		for i := range events {
			_, _ = fmt.Fprintf(w, "event: ping\ndata: %d\n\n", i)
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rec := newFlushRecorder()
	require.NoError(t, p.Get(rec, req))

	// Each upstream flush should produce at least one recorded chunk+flush on
	// the client side; buffering would collapse these into a single pair.
	require.GreaterOrEqual(t, rec.chunkCount(), events, "each upstream event should reach the writer as its own chunk")
	require.GreaterOrEqual(t, rec.flushCount(), events, "each upstream flush should trigger a downstream flush")
}

func TestProxy_Get_ForwardsRequest(t *testing.T) {
	t.Parallel()

	var gotMethod string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
	require.Contains(t, rr.Body.String(), "event: ping")
}

func TestProxy_Get_NonSSEResponseRelaysVerbatim(t *testing.T) {
	t.Parallel()

	// Per MCP § Listening for Messages from the Server, upstream MUST
	// respond with text/event-stream OR HTTP 405. The 405 path (and any
	// non-conformant 200 with a non-SSE body) must reach the client
	// verbatim — routing it through the SSE parser would silently mangle
	// or drop the body.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("server does not support GET streaming"))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, "text/plain", rr.Header().Get("Content-Type"))
	require.Equal(t, "POST, DELETE", rr.Header().Get("Allow"), "non-SSE response headers must reach the client")
	require.Equal(t, "server does not support GET streaming", rr.Body.String(), "non-SSE body must reach the client unmangled")
}

func TestProxy_Delete_ForwardsRequestWithSessionHeader(t *testing.T) {
	t.Parallel()

	var gotMethod, gotSession string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSession = r.Header.Get(proxy.McpSessionIDHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/x/mcp/id", http.NoBody)
	req.Header.Set(proxy.McpSessionIDHeader, "session-123")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Delete(rr, req))
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "session-123", gotSession)
	require.Equal(t, http.StatusNoContent, rr.Code)
}

func TestProxy_Post_HeadersPhaseTimeoutReturnsGatewayError(t *testing.T) {
	t.Parallel()

	// Upstream never writes headers, blocks until canceled. Phase 1 timer
	// fires and surfaces a CodeGatewayError "remote mcp server timed out".
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.NonStreamingTimeout = 50 * time.Millisecond

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
}

func TestProxy_Post_NonStreamingBodyPhaseTimeoutReturnsGatewayError(t *testing.T) {
	t.Parallel()

	// Upstream writes headers immediately but then holds the connection
	// open without writing the body. Phase 2 timer (post-headers) fires
	// and surfaces a gateway error — regression guard for the timer-reset
	// behavior in forwardRequest.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Block forever (until parent cancels) — body never arrives.
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.NonStreamingTimeout = 100 * time.Millisecond

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	start := time.Now()
	err := p.Post(rr, req)
	elapsed := time.Since(start)

	require.Error(t, err, "body-phase timeout must surface as a forward error")
	require.Less(t, elapsed, 1*time.Second, "phase-2 timer must fire well within the upstream's would-be wait")
}

func TestProxy_Get_LongStreamStaysAliveOnActivity(t *testing.T) {
	t.Parallel()

	// Upstream emits events with inter-event gaps shorter than
	// StreamingTimeout, accumulating to longer than NonStreamingTimeout.
	// Without the streaming-aware timeout policy, the headers-phase timer
	// would terminate the stream mid-flight; the per-event idle reset must
	// keep it alive.
	const eventCount = 6
	const interEventGap = 50 * time.Millisecond

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		for i := range eventCount {
			_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"step\":%d}}\n\n", i)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(interEventGap)
		}
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	// Both timeouts intentionally smaller than the total stream duration
	// (~300ms): NonStreamingTimeout would have killed the stream pre-fix;
	// per-event reset keeps StreamingTimeout from firing as long as
	// activity is at least every interEventGap.
	p.NonStreamingTimeout = 75 * time.Millisecond
	p.StreamingTimeout = 200 * time.Millisecond

	var observed []jsonrpc.Message
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "audit", observed: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, observed, eventCount, "every event must reach the interceptor — none lost to a premature timeout")
}

func TestProxy_Get_StreamTerminatesOnIdleTimeout(t *testing.T) {
	t.Parallel()

	// Upstream sends headers + one event, then goes silent. The
	// StreamingTimeout idle bound must fire and tear down the stream
	// even though NonStreamingTimeout is much larger.
	const idleTimeout = 100 * time.Millisecond

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"step\":0}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Hold the connection silent until the parent context cancels —
		// the proxy's idle timer should beat us to it.
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.NonStreamingTimeout = 5 * time.Second // deliberately too long to be load-bearing
	p.StreamingTimeout = idleTimeout

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	start := time.Now()
	_ = p.Get(rr, req) // an error here is acceptable; the stream was torn down mid-flight
	elapsed := time.Since(start)

	require.Less(t, elapsed, 1*time.Second, "idle stream must terminate within ~StreamingTimeout, not wait for NonStreamingTimeout")
	require.Contains(t, rr.Body.String(), `"step":0`, "first event must have reached the client before idle terminated the stream")
}

func TestProxy_Post_UpstreamUnreachableReturnsGatewayError(t *testing.T) {
	t.Parallel()

	// Bind to an available port then immediately release it so dialing fails.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	unreachableURL := srv.URL
	srv.Close()

	p := newProxyForTest(t, unreachableURL)
	p.NonStreamingTimeout = 500 * time.Millisecond

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
}

func TestProxy_Post_OversizedUpstreamBodyReturnsError(t *testing.T) {
	t.Parallel()

	// Upstream replies with a well-formed JSON-RPC result whose padding pushes
	// the total body past the configured cap — the parse-time allocation
	// guard trips before any bytes reach the client.
	padding := strings.Repeat("x", 2*1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"padding":"%s"}}`, padding)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.MaxBufferedBodyBytes = 512

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)
	require.ErrorIs(t, err, proxy.ErrBodyTooLarge)
}

func TestProxy_Post_OversizedUserBodyReturnsError(t *testing.T) {
	t.Parallel()

	// User sends a JSON-RPC request whose body exceeds the configured cap.
	// The proxy must reject before any upstream call is attempted.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when the user request body exceeds the cap")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.MaxBufferedBodyBytes = 128

	// Build a syntactically valid JSON-RPC request whose padding pushes the
	// total body past the cap.
	padding := strings.Repeat("x", 512)
	oversizedBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"padding":"%s"}}`, padding)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(oversizedBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	// The handler wraps ErrBodyTooLarge via oops.E(CodeBadRequest,...); the
	// underlying sentinel must still be reachable through the chain.
	require.ErrorIs(t, err, proxy.ErrBodyTooLarge)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestProxy_Get_LongStreamsAreNotSubjectToCumulativeCap(t *testing.T) {
	t.Parallel()

	// Stream emits many small SSE events whose individual sizes fit under
	// MaxBufferedBodyBytes but whose cumulative size exceeds it. The
	// streaming path must relay all of them — the cap applies per buffered
	// body / per event, not cumulatively across a stream.
	const eventCount = 100
	payloads := make([]string, eventCount)
	for i := range payloads {
		// Each event is a small JSON-RPC notification well under the cap.
		payloads[i] = `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p"}}`
	}
	body := sseBody(payloads...)
	require.Greater(t, len(body), 256, "test precondition: cumulative body must exceed the cap")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observed []jsonrpc.Message
	p := newProxyForTest(t, upstream.URL)
	p.MaxBufferedBodyBytes = 256 // each event ≤ 256 bytes; cumulative far exceeds
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "audit", observed: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, observed, eventCount, "all events must reach the interceptor regardless of cumulative size")
}

func TestProxy_Post_RecordsMetrics(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := proxy.NewMetrics(meterProvider.Meter("test"), discardLogger())

	p := newProxyForTest(t, upstream.URL)
	p.Metrics = metrics
	p.ServerID = "srv-abc"

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)

	byName := map[string]metricdata.Metrics{}
	for _, m := range rm.ScopeMetrics[0].Metrics {
		byName[m.Name] = m
	}

	requests, ok := byName[proxy.MeterRequests]
	require.True(t, ok, "requests counter must be recorded")
	metricdatatest.AssertHasAttributes(t, requests,
		attr.HTTPRequestMethod(http.MethodPost),
		attr.RemoteMCPProxyRemoteStatusClass("2xx"),
		attr.RemoteMCPServerID("srv-abc"),
	)

	duration, ok := byName[proxy.MeterRequestDuration]
	require.True(t, ok, "request duration histogram must be recorded")
	hist, ok := duration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)

	bytes, ok := byName[proxy.MeterResponseBytes]
	require.True(t, ok, "response bytes histogram must be recorded")
	bytesHist, ok := bytes.Data.(metricdata.Histogram[int64])
	require.True(t, ok)
	require.Len(t, bytesHist.DataPoints, 1)
	require.Positive(t, bytesHist.DataPoints[0].Sum, "response bytes sum must be > 0")
}

func TestProxy_Post_RecordsErrorStatusClassOnUpstreamFailure(t *testing.T) {
	t.Parallel()

	// Upstream bound, then closed, so the dial fails — there is no upstream
	// status and the metric must fall into the "error" class.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	unreachableURL := srv.URL
	srv.Close()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := proxy.NewMetrics(meterProvider.Meter("test"), discardLogger())

	p := newProxyForTest(t, unreachableURL)
	p.Metrics = metrics
	p.NonStreamingTimeout = 500 * time.Millisecond

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.Error(t, p.Post(rr, req))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var requests metricdata.Metrics
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == proxy.MeterRequests {
			requests = m
			break
		}
	}
	require.NotEmpty(t, requests.Name)
	metricdatatest.AssertHasAttributes(t, requests,
		attr.RemoteMCPProxyRemoteStatusClass("error"),
	)
}

func TestProxy_Post_ClientCancellationReturnsBadRequest(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	rr := httptest.NewRecorder()
	err := p.Post(rr, req)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

const toolsCallRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather","arguments":{"location":"sf"}}}`

func TestProxy_Post_ToolsCallRequestInterceptor_RunsForToolsCall(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sunny"}]}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsCallRequest
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mockToolsCallRequestInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed request interceptor must be invoked for tools/call")
	require.NotNil(t, observed.Params)
	require.Equal(t, "get_weather", observed.Params.Name)
	require.JSONEq(t, `{"location":"sf"}`, string(observed.Params.Arguments))
}

func TestProxy_Post_ToolsCallRequestInterceptor_SkipsForNonToolsCall(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	var called int
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mockToolsCallRequestInterceptor{name: "typed", called: &called},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Zero(t, called, "typed tools/call interceptor must not run for initialize")
}

func TestProxy_Post_ToolsCallRequestInterceptor_RejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when a typed interceptor rejects tools/call")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mockToolsCallRequestInterceptor{name: "typed", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "policy violation", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "typed rejection writes a JSON-RPC error envelope")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"id":1`, "envelope must preserve the originating tools/call request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate to the envelope")
	require.Contains(t, rr.Body.String(), "policy violation", "RejectError message must propagate to the envelope")
}

func TestProxy_Post_ToolsCallRequestInterceptor_RunsAfterGeneric(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sunny"}]}}`))
	}))
	t.Cleanup(upstream.Close)

	order := []string{}
	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "generic-req", order: &order},
	}
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
		&mockToolsCallRequestInterceptor{name: "typed-req", order: &order},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, []string{"generic-req", "typed-req"}, order, "generic interceptors must run before typed interceptors")
}

func TestProxy_Post_ToolsCallResponseInterceptor_RunsForSuccessResult(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sunny"}],"isError":false}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsCallResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observed, "typed response interceptor must be invoked for tools/call response")
	require.NotNil(t, observed.Result, "success response must populate Result")
	require.Nil(t, observed.Error)
	require.False(t, observed.Result.IsError)
	require.Len(t, observed.Result.Content, 1)
}

func TestProxy_Post_ToolsCallResponseInterceptor_RunsForJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid params"}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsCallResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observed, "typed response interceptor must run for JSON-RPC error responses too")
	require.Nil(t, observed.Result)
	require.NotNil(t, observed.Error)
	require.Equal(t, "invalid params", observed.Error.Message)
}

func TestProxy_Post_ToolsCallResponseInterceptor_RunsAfterGeneric(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	order := []string{}
	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "generic-resp", order: &order},
	}
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed-resp", order: &order},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, []string{"generic-resp", "typed-resp"}, order, "generic response interceptors must run before typed ones")
}

func TestProxy_Post_ToolsCallResponseInterceptor_RejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sensitive-payload"}]}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "response blocked", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "typed response rejection writes a JSON-RPC error envelope")
	require.Equal(t, http.StatusOK, rr.Code)
	require.NotContains(t, rr.Body.String(), "sensitive-payload", "rejected upstream payload must not reach the client")
	require.Contains(t, rr.Body.String(), `"id":1`, "envelope must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate")
	require.Contains(t, rr.Body.String(), "response blocked", "RejectError message must propagate")
}

// sseBody builds a canonical SSE body from a list of JSON-RPC payloads. Each
// payload becomes a single event with one "data:" line followed by the
// mandatory blank-line separator.
func sseBody(payloads ...string) string {
	var b strings.Builder
	for _, p := range payloads {
		b.WriteString("data: ")
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestProxy_Post_SSEResponse_FiresRemoteMessageInterceptorPerEvent(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.1}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"done"}]}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observed []jsonrpc.Message
	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "audit", observed: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"done"`, "stream body must reach the client")

	require.Len(t, observed, 3, "interceptor must fire once per parseable SSE event")
}

func TestProxy_Post_SSEResponse_TerminalEventDispatchesTypedInterceptor(t *testing.T) {
	t.Parallel()

	// Terminal event shares the request ID from toolsCallRequest (id=1). A
	// non-terminal progress notification must not trigger the typed
	// interceptor.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sunny"}],"isError":false}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observedTerminal *proxy.ToolsCallResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed", lastCall: &observedTerminal},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observedTerminal, "typed tools/call response interceptor must fire on terminal SSE event")
	require.NotNil(t, observedTerminal.Result, "terminal event must populate Result")
	require.Len(t, observedTerminal.Result.Content, 1)
}

func TestProxy_Post_SSEResponse_NonTerminalEventsDoNotDispatchTypedInterceptor(t *testing.T) {
	t.Parallel()

	// Stream contains only non-terminal events. Typed interceptor must not
	// fire — no event matches the originating request ID with a result.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.1}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var called int
	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "typed", called: &called},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Zero(t, called, "typed interceptor must not fire without a terminal event matching the request ID")
}

func TestProxy_Post_SSEResponse_RemoteMessageInterceptorRejectionSubstitutesEvent(t *testing.T) {
	t.Parallel()

	// Interceptor rejects every message it sees. The stream must continue
	// to be parsed; rejected events must be replaced with spec-aligned
	// JSON-RPC substitutes so the user's MCP runtime stays consistent.
	// - The progress notification (no id) becomes a notifications/message
	//   log entry at level=error.
	// - The terminal response (id=1) becomes a JSON-RPC error response
	//   with the same id.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"done"}]}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var called int
	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "blocker", called: &called, err: errors.New("policy rejection")},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "rejection must not abort the stream — substitution keeps the stream consistent")
	require.Equal(t, 2, called, "interceptor must fire for every parseable event, even after rejecting")

	// Originals must not reach the client.
	require.NotContains(t, rr.Body.String(), `"done"`, "rejected terminal event content must not reach the client")
	require.NotContains(t, rr.Body.String(), `"progress":0.5`, "rejected progress notification content must not reach the client")

	// Substitutes must be present.
	require.Contains(t, rr.Body.String(), `"notifications/message"`, "rejected notification must be replaced with a notifications/message log entry")
	require.Contains(t, rr.Body.String(), `"error"`, "substitute log notification must carry level=error")
	require.Contains(t, rr.Body.String(), `"id":1`, "rejected response substitute must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32603`, "default mapping for plain error rejections is RejectCodeInternalError")
}

func TestProxy_Get_SSE_FiresRemoteMessageInterceptors(t *testing.T) {
	t.Parallel()

	// GET streams are pure server-initiated events. The event interceptor
	// should fire for each event the same way as on POST SSE responses.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/resources/updated","params":{"uri":"file:///x"}}`,
		`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observed []jsonrpc.Message
	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "audit", observed: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))
	require.Len(t, observed, 2)
}

func TestProxy_Post_SSEResponse_TypedRejectionSubstitutesTerminalEvent(t *testing.T) {
	t.Parallel()

	// Stream contains a non-terminal progress event followed by a terminal
	// tools/call response. The typed interceptor rejects only the terminal
	// event. Progress event must still reach the client; terminal content
	// must not, but the client must see a substitute JSON-RPC error
	// response carrying the same id so its correlator can complete.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"sensitive-payload"}]}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
		&mockToolsCallResponseInterceptor{name: "pii-blocker", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "policy: contains sensitive content", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsCallRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Contains(t, rr.Body.String(), `"progress":0.5`, "non-terminal progress event must still relay")
	require.NotContains(t, rr.Body.String(), "sensitive-payload", "rejected terminal payload must not reach the client")
	require.Contains(t, rr.Body.String(), `"id":1`, "substitute must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate to the substitute")
	require.Contains(t, rr.Body.String(), "policy: contains sensitive content", "RejectError message must propagate to the substitute")
}

func TestProxy_Post_JSONResponse_RemoteMessageInterceptorRejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	// On the JSON path, headers are not yet sent when the interceptor runs,
	// so the proxy substitutes a JSON-RPC error envelope carrying the
	// originating request's id. The user's MCP runtime correlates and
	// surfaces the rejection cleanly.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"sensitive":"data"}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "blocker", err: errors.New("policy rejection")},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code, "rejected response with originating id gets HTTP 200 + JSON-RPC error body")
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.NotContains(t, rr.Body.String(), "sensitive", "rejected upstream response must not reach the client")
	require.Contains(t, rr.Body.String(), `"id":1`, "envelope must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32603`, "default mapping for plain error rejections")
}

func TestProxy_Post_NotificationRejectionWritesHTTP400IDLessEnvelope(t *testing.T) {
	t.Parallel()

	// MCP § Streamable HTTP: when the server rejects an inbound notification
	// (no id), it MUST return an HTTP error status code, with body that MAY
	// be a JSON-RPC error response with no id. The proxy implements the
	// MAY: HTTP 400 + id-less envelope.
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when an interceptor rejects the notification")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "blocker", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "blocked notification", Data: nil}},
	}

	const notificationBody = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(notificationBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusBadRequest, rr.Code, "rejected notifications get HTTP 4xx per MCP spec")
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.NotContains(t, rr.Body.String(), `"id"`, "rejected notification envelope must not carry an id field")
	require.Contains(t, rr.Body.String(), `"error"`)
	require.Contains(t, rr.Body.String(), `"code":-32000`)
	require.Contains(t, rr.Body.String(), "blocked notification")
}

const toolsListRequest = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":"page-1"}}`

func TestProxy_Post_ToolsListRequestInterceptor_RunsForToolsList(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_weather"}]}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsListRequest
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListRequestInterceptors = []proxy.ToolsListRequestInterceptor{
		&mockToolsListRequestInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed request interceptor must be invoked for tools/list")
	require.NotNil(t, observed.Params)
	require.Equal(t, "page-1", observed.Params.Cursor)
}

func TestProxy_Post_ToolsListRequestInterceptor_DecodesMissingParams(t *testing.T) {
	t.Parallel()

	// Per MCP spec, tools/list params may be omitted entirely. The typed
	// view must still invoke with a zero-valued params struct rather than
	// failing the decode and skipping the typed loop.
	const body = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsListRequest
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListRequestInterceptors = []proxy.ToolsListRequestInterceptor{
		&mockToolsListRequestInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.NotNil(t, observed, "typed request interceptor must run even when params are omitted")
	require.NotNil(t, observed.Params)
	require.Empty(t, observed.Params.Cursor)
}

func TestProxy_Post_ToolsListRequestInterceptor_SkipsForNonToolsList(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	var called int
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListRequestInterceptors = []proxy.ToolsListRequestInterceptor{
		&mockToolsListRequestInterceptor{name: "typed", called: &called},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(initializeRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Zero(t, called, "typed tools/list interceptor must not run for initialize")
}

func TestProxy_Post_ToolsListRequestInterceptor_RejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when a typed interceptor rejects tools/list")
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsListRequestInterceptors = []proxy.ToolsListRequestInterceptor{
		&mockToolsListRequestInterceptor{name: "typed", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "discovery blocked", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "typed rejection writes a JSON-RPC error envelope")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"id":2`, "envelope must preserve the originating tools/list request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate to the envelope")
	require.Contains(t, rr.Body.String(), "discovery blocked", "RejectError message must propagate to the envelope")
}

func TestProxy_Post_ToolsListRequestInterceptor_RunsAfterGeneric(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	order := []string{}
	p := newProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{name: "generic-req", order: &order},
	}
	p.ToolsListRequestInterceptors = []proxy.ToolsListRequestInterceptor{
		&mockToolsListRequestInterceptor{name: "typed-req", order: &order},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, []string{"generic-req", "typed-req"}, order, "generic interceptors must run before typed interceptors")
}

func TestProxy_Post_ToolsListResponseInterceptor_RunsForSuccessResult(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_weather"},{"name":"set_thermostat"}],"nextCursor":"page-2"}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsListResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observed, "typed response interceptor must be invoked for tools/list response")
	require.NotNil(t, observed.Result, "success response must populate Result")
	require.Nil(t, observed.Error)
	require.Equal(t, "page-2", observed.Result.NextCursor)
	require.Len(t, observed.Result.Tools, 2)
}

func TestProxy_Post_ToolsListResponseInterceptor_RunsForJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`))
	}))
	t.Cleanup(upstream.Close)

	var observed *proxy.ToolsListResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed", lastCall: &observed},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observed, "typed response interceptor must run for JSON-RPC error responses too")
	require.Nil(t, observed.Result)
	require.NotNil(t, observed.Error)
	require.Equal(t, "method not found", observed.Error.Message)
}

func TestProxy_Post_ToolsListResponseInterceptor_RunsAfterGeneric(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	order := []string{}
	p := newProxyForTest(t, upstream.URL)
	p.RemoteMessageInterceptors = []proxy.RemoteMessageInterceptor{
		&mockRemoteMessageInterceptor{name: "generic-resp", order: &order},
	}
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed-resp", order: &order},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, []string{"generic-resp", "typed-resp"}, order, "generic response interceptors must run before typed ones")
}

func TestProxy_Post_ToolsListResponseInterceptor_RejectionWritesJSONRPCError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"forbidden_tool"}]}}`))
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "listing blocked", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req), "typed response rejection writes a JSON-RPC error envelope")
	require.Equal(t, http.StatusOK, rr.Code)
	require.NotContains(t, rr.Body.String(), "forbidden_tool", "rejected upstream payload must not reach the client")
	require.Contains(t, rr.Body.String(), `"id":2`, "envelope must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate")
	require.Contains(t, rr.Body.String(), "listing blocked", "RejectError message must propagate")
}

func TestProxy_Post_SSEResponse_TerminalEventDispatchesTypedToolsListInterceptor(t *testing.T) {
	t.Parallel()

	// Terminal event shares the request ID from toolsListRequest (id=2). A
	// non-terminal progress notification must not trigger the typed
	// interceptor.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_weather"}]}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	var observedTerminal *proxy.ToolsListResponse
	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed", lastCall: &observedTerminal},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.NotNil(t, observedTerminal, "typed tools/list response interceptor must fire on terminal SSE event")
	require.NotNil(t, observedTerminal.Result, "terminal event must populate Result")
	require.Len(t, observedTerminal.Result.Tools, 1)
}

func TestProxy_Post_SSEResponse_TypedToolsListRejectionSubstitutesTerminalEvent(t *testing.T) {
	t.Parallel()

	// Stream contains a non-terminal progress event followed by a terminal
	// tools/list response. The typed interceptor rejects only the terminal
	// event. Progress event must still reach the client; terminal content
	// must not, but the client must see a substitute JSON-RPC error
	// response carrying the same id so its correlator can complete.
	body := sseBody(
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"forbidden_tool"}]}}`,
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "policy-blocker", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "policy: tool not allowed", Data: nil}},
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(toolsListRequest))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))

	require.Contains(t, rr.Body.String(), `"progress":0.5`, "non-terminal progress event must still relay")
	require.NotContains(t, rr.Body.String(), "forbidden_tool", "rejected terminal payload must not reach the client")
	require.Contains(t, rr.Body.String(), `"id":2`, "substitute must preserve the originating request id")
	require.Contains(t, rr.Body.String(), `"code":-32000`, "RejectError code must propagate to the substitute")
	require.Contains(t, rr.Body.String(), "policy: tool not allowed", "RejectError message must propagate to the substitute")
}
