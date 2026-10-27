package proxy_test

import (
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
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// drainTestTimeout bounds every wait in these tests. Drained streams end in
// milliseconds, so reaching it means the stream was never closed.
const drainTestTimeout = 5 * time.Second

// heldSSEUpstream answers every request with SSE headers and one notification,
// then holds the stream open until the proxy cancels its request. opened
// closes once the notification is flushed and canceled once the upstream sees
// the proxy go away.
type heldSSEUpstream struct {
	server   *httptest.Server
	opened   chan struct{}
	canceled chan struct{}
}

func newHeldSSEUpstream(t *testing.T) *heldSSEUpstream {
	t.Helper()

	u := &heldSSEUpstream{server: nil, opened: make(chan struct{}), canceled: make(chan struct{})}
	release := make(chan struct{})
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request like a real MCP server would: net/http notices a
		// client going away only once the request body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
		_ = http.NewResponseController(w).Flush()
		close(u.opened)
		select {
		case <-r.Context().Done():
			close(u.canceled)
		case <-release:
		}
	}))
	t.Cleanup(u.server.Close)
	// Runs before server.Close (LIFO) so a handler the test never drained
	// does not block shutdown.
	t.Cleanup(func() { close(release) })

	return u
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(drainTestTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// runAsync calls handle in a goroutine and returns a channel that delivers its
// error once it returns.
func runAsync(handle func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- handle() }()
	return done
}

func waitForHandler(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(drainTestTimeout):
		t.Fatal("timed out waiting for the proxy handler to return")
		return nil
	}
}

func TestProxy_Get_DrainClosesListenStreamWithRetry(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	drain := proxy.NewDrain()
	p := newProxyForTest(t, upstream.server.URL)
	p.Drain = drain

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Get(rr, req) })
	waitFor(t, upstream.opened, "the upstream stream to open")
	drain.Start()

	require.NoError(t, waitForHandler(t, done), "a stream closed for shutdown ends cleanly")
	waitFor(t, upstream.canceled, "the upstream request to be cancelled")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "\n\nretry: ", "the stream must end with an SSE retry field")
	require.NotContains(t, rr.Body.String(), "id:", "the proxy must not write SSE event ids of its own")
}

func TestProxy_Get_DrainStartedBeforeStreamClosesItImmediately(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	drain := proxy.NewDrain()
	drain.Start()
	p := newProxyForTest(t, upstream.server.URL)
	p.Drain = drain

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	require.NoError(t, waitForHandler(t, runAsync(func() error { return p.Get(rr, req) })))
	require.Contains(t, rr.Body.String(), "retry: ")
}

func TestProxy_Post_DrainEndsSubscriptionsListenWithCompletion(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	drain := proxy.NewDrain()
	p := newProxyForTest(t, upstream.server.URL)
	p.Drain = drain

	body := `{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true}}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Post(rr, req) })
	waitFor(t, upstream.opened, "the upstream stream to open")
	drain.Start()

	require.NoError(t, waitForHandler(t, done))
	waitFor(t, upstream.canceled, "the upstream subscription to be cancelled")

	events := sseDataEvents(t, rr.Body.String())
	require.NotEmpty(t, events)
	var completion struct {
		ID     int `json:"id"`
		Result struct {
			ResultType string         `json:"resultType"`
			Meta       map[string]int `json:"_meta"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(events[len(events)-1]), &completion))
	require.Equal(t, 7, completion.ID)
	require.Equal(t, "complete", completion.Result.ResultType)
	require.Equal(t, map[string]int{"io.modelcontextprotocol/subscriptionId": 7}, completion.Result.Meta)
	require.NotContains(t, rr.Body.String(), "notifications/cancelled", "on HTTP the server ends a subscription with its result, not a cancellation")
}

func TestProxy_Post_DrainLeavesToolCallStreamOpen(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	opened := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":1}}\n\n")
		_ = http.NewResponseController(w).Flush()
		close(opened)
		<-release
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[]}}\n\n")
	}))
	t.Cleanup(upstream.Close)

	drain := proxy.NewDrain()
	p := newProxyForTest(t, upstream.URL)
	p.Drain = drain

	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"slow","arguments":{}}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	done := runAsync(func() error { return p.Post(rr, req) })
	waitFor(t, opened, "the upstream stream to open")
	drain.Start()
	close(release)

	require.NoError(t, waitForHandler(t, done))
	require.Contains(t, rr.Body.String(), `"id":3,"result"`, "a tool call in flight must still receive its response")
	require.NotContains(t, rr.Body.String(), "retry: ")
	require.NotContains(t, rr.Body.String(), "resultType")
}

func TestProxy_Post_RecordsStreamEnd(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "id: evt-1\ndata: {\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{}}\n\n")
	}))
	t.Cleanup(upstream.Close)

	reader := sdkmetric.NewManualReader()
	p := newProxyForTest(t, upstream.URL)
	p.Metrics = proxy.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	streams := collectMetric(t, reader, proxy.MeterStreams)
	metricdatatest.AssertHasAttributes(t, streams,
		attr.HTTPRequestMethod(http.MethodPost),
		attr.McpMethod("ping"),
		attr.RemoteMCPProxyStreamEndReason(string(proxy.StreamEndUpstreamClosed)),
		attr.RemoteMCPProxyStreamResponseRelayed(true),
		attr.RemoteMCPProxyStreamResumable(true),
	)
}

func TestProxy_Get_RecordsShutdownStreamEnd(t *testing.T) {
	t.Parallel()

	upstream := newHeldSSEUpstream(t)
	reader := sdkmetric.NewManualReader()
	drain := proxy.NewDrain()
	p := newProxyForTest(t, upstream.server.URL)
	p.Drain = drain
	p.Metrics = proxy.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "evt-9")

	done := runAsync(func() error { return p.Get(httptest.NewRecorder(), req) })
	waitFor(t, upstream.opened, "the upstream stream to open")
	drain.Start()
	require.NoError(t, waitForHandler(t, done))

	streams := collectMetric(t, reader, proxy.MeterStreams)
	metricdatatest.AssertHasAttributes(t, streams,
		attr.HTTPRequestMethod(http.MethodGet),
		attr.RemoteMCPProxyStreamEndReason(string(proxy.StreamEndShutdown)),
		attr.RemoteMCPProxyStreamResumeRequested(true),
	)

	duration := collectMetric(t, reader, proxy.MeterStreamDuration)
	hist, ok := duration.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
}

func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %s was not recorded", name)
	return metricdata.Metrics{}
}

// sseDataEvents returns the data payload of each event in an SSE body.
func sseDataEvents(t *testing.T, body string) []string {
	t.Helper()

	var events []string
	for line := range strings.Lines(body) {
		if data, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "data: "); ok {
			events = append(events, data)
		}
	}
	return events
}
