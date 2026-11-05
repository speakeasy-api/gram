package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/httpstream"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// testMaxStreamAge is the maximum stream age these tests use. It is long
// enough for the upstream stream to open first and short enough that a stream
// the proxy fails to rotate trips drainTestTimeout instead.
const testMaxStreamAge = 200 * time.Millisecond

func TestProxy_Get_MaxAgeClosesListenStreamWithRetry(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	p := newProxyForTest(t, upstream.server.URL)
	p.MaxStreamAge = testMaxStreamAge

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Get(rr, req) })
	waitFor(t, upstream.opened, "the upstream stream to open")

	require.NoError(t, waitForHandler(t, done), "a stream closed at its maximum age ends cleanly")
	waitFor(t, upstream.canceled, "the upstream request to be cancelled")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "\n\nretry: ", "the stream must end with an SSE retry field")
	require.NotContains(t, rr.Body.String(), "id:", "the proxy must not write SSE event ids of its own")
}

func TestProxy_Get_MaxAgeDisabledLeavesStreamOpen(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	p := newProxyForTest(t, upstream.server.URL)
	p.MaxStreamAge = 0

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Get(rr, req) })
	waitFor(t, upstream.opened, "the upstream stream to open")

	select {
	case err := <-done:
		t.Fatalf("the stream ended without a maximum age: %v", err)
	case <-time.After(2 * testMaxStreamAge):
	}

	// The recorder is only read once the handler has returned.
	cancel()
	require.Error(t, waitForHandler(t, done), "a stream whose client went away is reported")
	require.NotContains(t, rr.Body.String(), "retry: ")
}

func TestProxy_Post_MaxAgeEndsSubscriptionsListenWithCompletion(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	p := newProxyForTest(t, upstream.server.URL)
	p.MaxStreamAge = testMaxStreamAge

	body := `{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true}}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Post(rr, req) })
	waitFor(t, upstream.opened, "the upstream stream to open")

	require.NoError(t, waitForHandler(t, done))
	waitFor(t, upstream.canceled, "the upstream subscription to be cancelled")

	events := sseDataEvents(t, rr.Body.String())
	require.NotEmpty(t, events)
	var completion struct {
		ID     int `json:"id"`
		Result struct {
			ResultType string `json:"resultType"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(events[len(events)-1]), &completion))
	require.Equal(t, 7, completion.ID)
	require.Equal(t, "complete", completion.Result.ResultType)
	require.NotContains(t, rr.Body.String(), "retry: ", "a POST stream ends with its result, not a retry field")
}

func TestProxy_Post_MaxAgeWaitsForToolCallResponse(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	opened := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request like a real MCP server would: net/http notices a
		// client going away only once the request body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":1}}\n\n")
		_ = http.NewResponseController(w).Flush()
		close(opened)
		<-release
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[]}}\n\n")
		_ = http.NewResponseController(w).Flush()
		// Hold the stream open after the response: the proxy must end it
		// itself now that the stream is past its maximum age.
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)

	reader := sdkmetric.NewManualReader()
	p := newProxyForTest(t, upstream.URL)
	p.MaxStreamAge = testMaxStreamAge
	p.Metrics = proxy.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))

	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"slow","arguments":{}}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Post(rr, req) })
	waitFor(t, opened, "the upstream stream to open")

	// The stream passes its maximum age while the tool call is still in
	// flight; it must stay open until the response has been relayed.
	select {
	case err := <-done:
		t.Fatalf("the stream ended before the tool call responded: %v", err)
	case <-time.After(2 * testMaxStreamAge):
	}
	close(release)

	require.NoError(t, waitForHandler(t, done))
	require.Contains(t, rr.Body.String(), `"id":3,"result"`, "a tool call in flight must still receive its response")
	require.NotContains(t, rr.Body.String(), "retry: ")
	require.NotContains(t, rr.Body.String(), "resultType")

	// The stream was rotated the moment its response was relayed.
	streams := collectMetric(t, reader, proxy.MeterStreams)
	metricdatatest.AssertHasAttributes(t, streams,
		attr.RemoteMCPProxyStreamEndReason(string(proxy.StreamEndMaxAge)),
		attr.RemoteMCPProxyStreamResponseRelayed(true),
	)
}

func TestProxy_Get_RecordsMaxAgeStreamEnd(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	reader := sdkmetric.NewManualReader()
	p := newProxyForTest(t, upstream.server.URL)
	p.MaxStreamAge = testMaxStreamAge
	p.Metrics = proxy.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	req.Pattern = "GET /mcp/{mcpSlug}"
	req.Header.Set("Accept", "text/event-stream")

	done := runAsync(func() error { return p.Get(httptest.NewRecorder(), req) })
	waitFor(t, upstream.opened, "the upstream stream to open")
	require.NoError(t, waitForHandler(t, done))

	streams := collectMetric(t, reader, proxy.MeterStreams)
	metricdatatest.AssertHasAttributes(t, streams,
		attr.HTTPRequestMethod(http.MethodGet),
		attr.RemoteMCPProxyStreamEndReason(string(proxy.StreamEndMaxAge)),
	)

	shared := collectMetric(t, reader, httpstream.MeterStreamDuration)
	metricdatatest.AssertHasAttributes(t, shared,
		attr.HTTPRoute("/mcp/{mcpSlug}"),
		attr.HTTPRequestMethod(http.MethodGet),
		attr.HTTPStreamEndReason(string(httpstream.EndMaxAge)),
	)
	hist, ok := shared.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	require.GreaterOrEqual(t, hist.DataPoints[0].Sum, testMaxStreamAge.Seconds(), "the shared histogram measures from request receipt")
}
