package externalmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// testMessageLimit is the per-message cap the tests configure, small enough
	// that oversized fixtures stay cheap.
	testMessageLimit = 16 << 10 // 16 KiB

	// endlessBodyCap stops a runaway endless response if a limit fails to
	// reject it, so a regression fails on the assertions instead of exhausting
	// the test host.
	endlessBodyCap = 32 << 20 // 32 MiB
)

// limitServer is a minimal Streamable HTTP MCP server. respond may take over
// the answer for any method; everything it declines gets a small valid result.
type limitServer struct {
	respond func(w http.ResponseWriter, method string, id json.RawMessage) bool
}

func (s limitServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if s.respond != nil && s.respond(w, envelope.Method, envelope.ID) {
		return
	}
	if len(envelope.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var result any
	switch envelope.Method {
	case methodServerDiscover:
		// Refusing the probe sends the SDK to the legacy initialize handshake.
		w.WriteHeader(http.StatusInternalServerError)
		return
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "limit", "version": "1.0.0"},
		}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": map[string]any{"type": "object"}}}}
	case "tools/call":
		result = callResult("ok")
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", "limit-session")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "result": result})
}

func callResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

// writeEndless answers with status and contentType, then streams prefix and
// filler without a Content-Length until the client hangs up.
func writeEndless(w http.ResponseWriter, status int, contentType, prefix string, written *atomic.Int64) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Mcp-Session-Id", "limit-session")
	w.WriteHeader(status)
	rc := http.NewResponseController(w)
	if _, err := io.WriteString(w, prefix); err != nil {
		return
	}
	chunk := bytes.Repeat([]byte("a"), 32<<10)
	for written.Load() < endlessBodyCap {
		n, err := w.Write(chunk)
		written.Add(int64(n))
		if err != nil || rc.Flush() != nil {
			return
		}
	}
}

func jsonPrefix(id json.RawMessage) string {
	return `{"jsonrpc":"2.0","id":` + string(id) + `,"result":{"pad":"`
}

func sseEvent(t *testing.T, id json.RawMessage, method string, payload any) string {
	t.Helper()
	message := map[string]any{"jsonrpc": "2.0"}
	if id != nil {
		message["id"] = id
		message["result"] = payload
	} else {
		message["method"] = method
		message["params"] = payload
	}
	data, err := json.Marshal(message)
	require.NoError(t, err)
	return "event: message\ndata: " + string(data) + "\n\n"
}

func newLimitClient(t *testing.T, ctx context.Context, handler http.Handler, transport types.TransportType, opts *ClientOptions) (*Client, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	c, err := NewClient(ctx, testenv.NewLogger(t), policy, server.URL, transport, opts)
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, err
}

func limitOptions(maxResponseBytes int64) *ClientOptions {
	return &ClientOptions{DisableRetries: true, MaxMessageBytes: testMessageLimit, MaxResponseBytes: maxResponseBytes}
}

func requireRejectedEndless(t *testing.T, ctx context.Context, err error, written *atomic.Int64) {
	t.Helper()
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.NoError(t, ctx.Err(), "the limit must reject the body, not the test deadline")
	require.Positive(t, written.Load(), "the oversized body must have reached the client")
}

func TestBodyLimitStopsReadingAfterRejection(t *testing.T) {
	t.Parallel()

	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			t.Parallel()

			// Inspect transport reads directly: server-side writes can run far
			// ahead of the client inside kernel socket buffers.
			content := "data: " + strings.Repeat("x", 4*testMessageLimit)
			source := strings.NewReader(content)
			transport := &bodyLimitRoundTripper{
				base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {contentType}},
						Body:       io.NopCloser(source),
					}, nil
				}),
				messageLimit: testMessageLimit,
			}
			req := httptest.NewRequest(http.MethodGet, "https://example.com/mcp", nil)
			response, err := transport.RoundTrip(req)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, response.Body.Close()) })

			var buffer [1024]byte
			for err == nil {
				_, err = response.Body.Read(buffer[:])
			}
			require.ErrorIs(t, err, ErrResponseTooLarge)
			require.True(t, transport.tripped.Load())
			remaining := source.Len()
			require.LessOrEqual(t, len(content)-remaining, testMessageLimit+len(buffer), "at most one read may cross the limit")

			_, err = io.Copy(io.Discard, response.Body)
			require.ErrorIs(t, err, ErrResponseTooLarge)
			require.Equal(t, remaining, source.Len(), "draining after rejection must not consume more of the source")
		})
	}
}

func TestNewClientRejectsOversizedHandshakeJSON(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != methodServerDiscover && method != "initialize" {
			return false
		}
		writeEndless(w, http.StatusOK, "application/json", jsonPrefix(id), &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.Nil(t, c)
	requireRejectedEndless(t, ctx, err, &written)
}

func TestListToolsRejectsOversizedJSON(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/list" {
			return false
		}
		writeEndless(w, http.StatusOK, "application/json", jsonPrefix(id), &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.NoError(t, err)

	tools, err := c.ListTools(ctx)
	require.Nil(t, tools)
	requireRejectedEndless(t, ctx, err, &written)
}

func TestCallToolRejectsOversizedJSON(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		writeEndless(w, http.StatusOK, "application/json", jsonPrefix(id), &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.NoError(t, err)

	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{}`), nil)
	require.Nil(t, result)
	requireRejectedEndless(t, ctx, err, &written)
}

// The SDK reads a non-2xx body whole to surface a JSON-RPC error, whatever its
// content type, so an error body labeled as an event stream gets the whole-body
// cap rather than the per-event one.
func TestCallToolRejectsOversizedErrorBody(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		writeEndless(w, http.StatusBadRequest, "text/event-stream", `{"jsonrpc":"2.0","id":`+string(id)+`,"error":{"code":-32600,"message":"`, &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.NoError(t, err)

	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{}`), nil)
	require.Nil(t, result)
	requireRejectedEndless(t, ctx, err, &written)
}

// Production callers leave the cap unset, which must still bound a message.
func TestListToolsDefaultLimitRejectsOversizedJSON(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/list" {
			return false
		}
		writeEndless(w, http.StatusOK, "application/json", jsonPrefix(id), &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, nil)
	require.NoError(t, err)

	tools, err := c.ListTools(ctx)
	require.Nil(t, tools)
	requireRejectedEndless(t, ctx, err, &written)
	require.Greater(t, written.Load(), int64(testMessageLimit), "the default cap must sit above the test cap")
}

// A long-lived stream may carry far more than one message's worth of bytes in
// total, as long as each event stays under the cap.
func TestCallToolAcceptsSSEStreamLargerThanMessageLimit(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		notification := sseEvent(t, nil, "notifications/message", map[string]any{"level": "info", "data": strings.Repeat("n", 1<<10)})
		for range 4 * testMessageLimit / len(notification) {
			_, _ = io.WriteString(w, notification)
		}
		_, _ = io.WriteString(w, sseEvent(t, id, "", callResult("streamed")))
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.NoError(t, err)

	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	require.JSONEq(t, `{"type":"text","text":"streamed"}`, string(result.Content[0]))
}

func TestCallToolRejectsOversizedSSEEvent(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		small := sseEvent(t, nil, "notifications/message", map[string]any{"level": "info", "data": "fits"})
		writeEndless(w, http.StatusOK, "text/event-stream", small+"event: message\ndata: "+jsonPrefix(id), &written)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(0))
	require.NoError(t, err)

	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{}`), nil)
	require.Nil(t, result)
	requireRejectedEndless(t, ctx, err, &written)
}

// Probes keep their aggregate cap: a stream of individually small events still
// fails once the whole response exceeds MaxResponseBytes.
func TestCallToolTotalLimitRejectsLongSSEStream(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/call" {
			return false
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		notification := sseEvent(t, nil, "notifications/message", map[string]any{"level": "info", "data": strings.Repeat("n", 1<<10)})
		for range 4 * testMessageLimit / len(notification) {
			_, _ = io.WriteString(w, notification)
		}
		_, _ = io.WriteString(w, sseEvent(t, id, "", callResult("streamed")))
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(2*testMessageLimit))
	require.NoError(t, err)

	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{}`), nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.NoError(t, ctx.Err())
}

func TestListToolsTotalLimitRejectsJSONUnderMessageLimit(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	server := limitServer{respond: func(w http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "tools/list" {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, jsonPrefix(id)+strings.Repeat("a", testMessageLimit/2)+`"}}`)
		return true
	}}

	c, err := newLimitClient(t, ctx, server, types.TransportTypeStreamableHTTP, limitOptions(testMessageLimit/4))
	require.NoError(t, err)

	tools, err := c.ListTools(ctx)
	require.Nil(t, tools)
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.NoError(t, ctx.Err())
}

// The legacy SSE transport reads its endpoint from the first event of the
// hanging GET, so connecting must already be bounded.
func TestNewClientRejectsOversizedLegacySSEEndpointEvent(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var written atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEndless(w, http.StatusOK, "text/event-stream", "event: endpoint\ndata: /", &written)
	})

	c, err := newLimitClient(t, ctx, handler, types.TransportTypeSSE, limitOptions(0))
	require.Nil(t, c)
	requireRejectedEndless(t, ctx, err, &written)
}

func TestSSEEventLimiter(t *testing.T) {
	t.Parallel()

	const limit = 32
	event := "data: " + strings.Repeat("x", 20) + "\n\n" // 28 bytes
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "many LF events under the limit", input: strings.Repeat(event, 100)},
		{name: "many CRLF events under the limit", input: strings.Repeat(strings.ReplaceAll(event, "\n", "\r\n"), 100)},
		{name: "blank line of only CR bytes ends an event", input: strings.Repeat("data: "+strings.Repeat("x", 20)+"\n\r\r\n", 100)},
		{name: "single event over the limit", input: event + "data: " + strings.Repeat("x", limit) + "\n\n", wantErr: true},
		{name: "multi-line event counts every line", input: "data: xxxxxxxx\ndata: xxxxxxxx\ndata: xxxxxxxx\n\n", wantErr: true},
		{name: "comment lines without a blank line count", input: strings.Repeat(": keepalive\n", 4), wantErr: true},
		{name: "unterminated line over the limit", input: "data: " + strings.Repeat("x", limit), wantErr: true},
	}
	for _, tc := range tests {
		for _, oneByte := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/oneByte=%t", tc.name, oneByte), func(t *testing.T) {
				t.Parallel()
				var r io.Reader = strings.NewReader(tc.input)
				if oneByte {
					r = iotest.OneByteReader(r)
				}
				limiter := &sseEventLimiter{ReadCloser: io.NopCloser(r), limit: limit, eventBytes: 0, lineBlank: true, err: nil}

				got, err := io.ReadAll(limiter)
				if !tc.wantErr {
					require.NoError(t, err)
					require.Equal(t, tc.input, string(got))
					return
				}
				var maxBytes *http.MaxBytesError
				require.ErrorAs(t, err, &maxBytes)
				require.EqualValues(t, limit, maxBytes.Limit)
				require.True(t, strings.HasPrefix(tc.input, string(got)), "bytes before the overflow pass through unchanged")

				_, err = limiter.Read(make([]byte, 1))
				require.ErrorAs(t, err, &maxBytes, "the limit stays tripped")
			})
		}
	}
}
