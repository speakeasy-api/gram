package proxy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// toolsListLabelledUpstream declares its own public, long-lived cache stance
// and carries a per-tool member the SDK does not model, so tests can assert
// the label overwrites the former and leaves the latter's bytes alone.
const toolsListLabelledUpstream = `{"jsonrpc":"2.0","id":2,"result":{"ttlMs":60000,"cacheScope":"public","tools":[{"name":"a","inputSchema":{},"x-vendor":"keep"}]}}`

func jsonUpstream(t *testing.T, body string) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

func sseUpstream(t *testing.T, body string) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

// relayedResult decodes the result members of a relayed JSON-RPC response.
func relayedResult(t *testing.T, message string) map[string]json.RawMessage {
	t.Helper()
	var envelope struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(message), &envelope))
	require.NotNil(t, envelope.Result, "relayed message must carry a result: %s", message)
	return envelope.Result
}

// sseDataPayloads returns the data payload of each event in an SSE body.
func sseDataPayloads(body string) []string {
	var payloads []string
	for event := range strings.SplitSeq(body, "\n\n") {
		for line := range strings.SplitSeq(event, "\n") {
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				payloads = append(payloads, data)
			}
		}
	}
	return payloads
}

func requireCallerVaryingResult(t *testing.T, result map[string]json.RawMessage) {
	t.Helper()
	require.JSONEq(t, `"private"`, string(result["cacheScope"]),
		"a proxied list result must never fall back to the public cache default")
	require.JSONEq(t, `0`, string(result["ttlMs"]),
		"a proxied list result must not inherit an upstream ttl")
}

// TestProxy_Post_ToolsListLabelledWithoutInterceptors covers the public
// visibility shape: no tools/list response interceptor is attached, so only
// the proxy itself can label the result.
func TestProxy_Post_ToolsListLabelledWithoutInterceptors(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t, toolsListLabelledUpstream))
	require.Empty(t, p.ToolsListResponseInterceptors)

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	result := relayedResult(t, rr.Body.String())
	requireCallerVaryingResult(t, result)
	require.JSONEq(t, `[{"name":"a","inputSchema":{},"x-vendor":"keep"}]`, string(result["tools"]))
	require.Contains(t, string(result["tools"]), `"x-vendor":"keep"`, "the tools member must keep its original values")
}

func TestProxy_Post_ResourcesListLabelledWithoutInterceptors(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t,
		`{"jsonrpc":"2.0","id":4,"result":{"cacheScope":"public","resources":[{"name":"a","uri":"file:///a","x-vendor":"keep"}]}}`))
	require.Empty(t, p.ResourcesListResponseInterceptors)

	rr, err := postJSON(t, p, resourcesListRequest)
	require.NoError(t, err)

	result := relayedResult(t, rr.Body.String())
	requireCallerVaryingResult(t, result)
	require.Contains(t, string(result["resources"]), `"x-vendor":"keep"`, "the resources member must keep its original values")
}

// TestProxy_Post_ToolsListLabelledAfterInterceptorMutation covers a result an
// interceptor rewrote: SetTools touches only the tools member, so the label
// must still land on the rewritten result.
func TestProxy_Post_ToolsListLabelledAfterInterceptorMutation(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t, toolsListLabelledUpstream))
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mutatingToolsListResponseInterceptor{name: "drop-all", toolsFn: func([]*mcp.Tool) []*mcp.Tool { return nil }, err: nil},
	}

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	result := relayedResult(t, rr.Body.String())
	requireCallerVaryingResult(t, result)
	require.JSONEq(t, `[]`, string(result["tools"]))
}

// TestProxy_Post_ToolsListLabelsResultThatFailsTypedDecode covers a non-strict
// proxy relaying a result the typed view rejects: it still carries a catalog
// a lenient client may read, so it must not relay unlabelled.
func TestProxy_Post_ToolsListLabelsResultThatFailsTypedDecode(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"a","inputSchema":{},"annotations":{"readOnlyHint":"true"}}]}}`))
	var called int
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "typed", called: &called},
	}

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	require.Zero(t, called, "fixture must fail the typed decode")
	result := relayedResult(t, rr.Body.String())
	requireCallerVaryingResult(t, result)
	require.Contains(t, string(result["tools"]), `"readOnlyHint":"true"`)
}

// TestProxy_Post_ToolsListWithUndecodableParamsLabelled covers a request no
// typed view is built for: the label gates on the method alone.
func TestProxy_Post_ToolsListWithUndecodableParamsLabelled(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t, toolsListLabelledUpstream))

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":123}}`)
	require.NoError(t, err)

	requireCallerVaryingResult(t, relayedResult(t, rr.Body.String()))
}

// TestProxy_Post_ToolsListResultAlongsideErrorLabelled covers a malformed
// response the typed view reads as an error, so no filter touches it: a
// lenient client may still read its result.
func TestProxy_Post_ToolsListResultAlongsideErrorLabelled(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t,
		`{"jsonrpc":"2.0","id":2,"result":{"cacheScope":"public","tools":[{"name":"a","inputSchema":{}}]},"error":{"code":1,"message":"x"}}`))

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	requireCallerVaryingResult(t, relayedResult(t, rr.Body.String()))
}

// TestProxy_Post_DuplicateMethodListRequestLabelled covers a request whose
// decoded method is not a list method, while an upstream honouring the first
// method member still answers with a list.
func TestProxy_Post_DuplicateMethodListRequestLabelled(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t, toolsListLabelledUpstream))

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":2,"method":"tools/list","method":"ping"}`)
	require.NoError(t, err)

	requireCallerVaryingResult(t, relayedResult(t, rr.Body.String()))
}

// TestProxy_Post_ListResponsesWithoutResultObjectRelayVerbatim covers the
// responses the label leaves alone: they carry no result to label, and
// splicing would turn a null result into an object.
func TestProxy_Post_ListResponsesWithoutResultObjectRelayVerbatim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		request  string
		upstream string
	}{
		{
			name:     "tools/list error",
			request:  toolsListRequest,
			upstream: `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`,
		},
		{
			name:     "resources/list error",
			request:  resourcesListRequest,
			upstream: `{"jsonrpc":"2.0","id":4,"error":{"code":-32601,"message":"method not found"}}`,
		},
		{
			name:     "tools/list null result",
			request:  toolsListRequest,
			upstream: `{"jsonrpc":"2.0","id":2,"result":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := newProxyForTest(t, jsonUpstream(t, tc.upstream))

			rr, err := postJSON(t, p, tc.request)
			require.NoError(t, err)

			require.Equal(t, tc.upstream, rr.Body.String())
		})
	}
}

func TestProxy_Post_NonListResultNotLabelled(t *testing.T) {
	t.Parallel()

	upstream := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`
	p := newProxyForTest(t, jsonUpstream(t, upstream))

	rr, err := postJSON(t, p, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a","arguments":{}}}`)
	require.NoError(t, err)

	require.Equal(t, upstream, rr.Body.String())
}

// TestProxy_Post_StrictToolsListUndecodableResultStillFailsClosed pins the
// ordering on the buffered path: labelling an undecodable result must not
// let it past the strict fail-closed check that runs afterwards.
func TestProxy_Post_StrictToolsListUndecodableResultStillFailsClosed(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, jsonUpstream(t,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"secret","annotations":{"readOnlyHint":"true"}}]}}`))
	p.StrictToolSelection = true

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	require.Contains(t, rr.Body.String(), "unreadable tools/list response")
	require.NotContains(t, rr.Body.String(), "secret")
}

func TestProxy_Post_SSEListResultsLabelled(t *testing.T) {
	t.Parallel()

	const progress = `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`
	cases := []struct {
		name     string
		request  string
		terminal string
		list     string
	}{
		{
			name:     "tools/list",
			request:  toolsListRequest,
			terminal: toolsListLabelledUpstream,
			list:     "tools",
		},
		{
			name:     "resources/list",
			request:  resourcesListRequest,
			terminal: `{"jsonrpc":"2.0","id":4,"result":{"cacheScope":"public","resources":[{"name":"a","uri":"file:///a"}]}}`,
			list:     "resources",
		},
		{
			name:     "tools/list with undecodable params",
			request:  `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":123}}`,
			terminal: toolsListLabelledUpstream,
			list:     "tools",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := newProxyForTest(t, sseUpstream(t, sseBody(progress, tc.terminal)))

			rr, err := postJSON(t, p, tc.request)
			require.NoError(t, err)

			payloads := sseDataPayloads(rr.Body.String())
			require.Len(t, payloads, 2)
			require.JSONEq(t, progress, payloads[0], "notifications carry no result and must relay unchanged")
			result := relayedResult(t, payloads[1])
			requireCallerVaryingResult(t, result)
			require.NotEmpty(t, result[tc.list])
		})
	}
}

// TestProxy_Post_SSEListStreamLabelsOnlyResults covers the other events a
// list stream may carry: a reply whose id does not match the request is
// labelled anyway, while server-to-client requests and error replies relay
// unchanged.
func TestProxy_Post_SSEListStreamLabelsOnlyResults(t *testing.T) {
	t.Parallel()

	const (
		serverRequest = `{"jsonrpc":"2.0","id":"s1","method":"sampling/createMessage","params":{"messages":[],"maxTokens":1}}`
		otherReply    = `{"jsonrpc":"2.0","id":99,"result":{"cacheScope":"public","tools":[]}}`
		errorReply    = `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`
	)
	p := newProxyForTest(t, sseUpstream(t, sseBody(serverRequest, otherReply, errorReply)))

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	payloads := sseDataPayloads(rr.Body.String())
	require.Len(t, payloads, 3)
	require.JSONEq(t, serverRequest, payloads[0])
	requireCallerVaryingResult(t, relayedResult(t, payloads[1]))
	require.JSONEq(t, errorReply, payloads[2])
}

// TestProxy_Get_ReplayedResultsLabelled covers a resumed GET stream replaying
// the reply to a list request: nothing on the stream says which request a
// reply answers, so every result is labelled while other events relay
// unchanged.
func TestProxy_Get_ReplayedResultsLabelled(t *testing.T) {
	t.Parallel()

	const (
		notification = `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`
		errorReply   = `{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"method not found"}}`
	)
	p := newProxyForTest(t, sseUpstream(t, sseBody(notification, toolsListLabelledUpstream, errorReply)))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x/mcp/id", http.NoBody)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Last-Event-ID", "1")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Get(rr, req))

	payloads := sseDataPayloads(rr.Body.String())
	require.Len(t, payloads, 3)
	require.JSONEq(t, notification, payloads[0])
	requireCallerVaryingResult(t, relayedResult(t, payloads[1]))
	require.JSONEq(t, errorReply, payloads[2])
}

// TestProxy_Post_SSEToolsListRejectionWinsOverLabel pins that the label never
// resurrects a rejected terminal event: the substitute error is relayed and
// none of the upstream result is.
func TestProxy_Post_SSEToolsListRejectionWinsOverLabel(t *testing.T) {
	t.Parallel()

	p := newProxyForTest(t, sseUpstream(t, sseBody(toolsListLabelledUpstream)))
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mockToolsListResponseInterceptor{name: "reject", err: &proxy.RejectError{Code: -32000, Message: "listing blocked", Data: nil}},
	}

	rr, err := postJSON(t, p, toolsListRequest)
	require.NoError(t, err)

	require.Contains(t, rr.Body.String(), "listing blocked")
	require.NotContains(t, rr.Body.String(), "cacheScope")
	require.NotContains(t, rr.Body.String(), `"x-vendor"`)
}

// TestProxy_Post_LabelOnlyMutationLogsAtDebug keeps the per-list label out of
// info logs while interceptor mutations, which are rare, stay visible there.
func TestProxy_Post_LabelOnlyMutationLogsAtDebug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		interceptors []proxy.ToolsListResponseInterceptor
		sse          bool
		message      string
		wantLevel    slog.Level
	}{
		{
			name:         "buffered label only",
			interceptors: nil,
			sse:          false,
			message:      "relaying mutated response body to client",
			wantLevel:    slog.LevelDebug,
		},
		{
			name: "buffered interceptor mutation",
			interceptors: []proxy.ToolsListResponseInterceptor{
				&mutatingToolsListResponseInterceptor{name: "drop-all", toolsFn: func([]*mcp.Tool) []*mcp.Tool { return nil }, err: nil},
			},
			sse:       false,
			message:   "relaying mutated response body to client",
			wantLevel: slog.LevelInfo,
		},
		{
			name:         "sse label only",
			interceptors: nil,
			sse:          true,
			message:      "relaying mutated SSE event to client",
			wantLevel:    slog.LevelDebug,
		},
		{
			name: "sse interceptor mutation",
			interceptors: []proxy.ToolsListResponseInterceptor{
				&mutatingToolsListResponseInterceptor{name: "drop-all", toolsFn: func([]*mcp.Tool) []*mcp.Tool { return nil }, err: nil},
			},
			sse:       true,
			message:   "relaying mutated SSE event to client",
			wantLevel: slog.LevelInfo,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstreamURL := jsonUpstream(t, toolsListLabelledUpstream)
			if tc.sse {
				upstreamURL = sseUpstream(t, sseBody(toolsListLabelledUpstream))
			}
			var logs bytes.Buffer
			p := newProxyForTest(t, upstreamURL)
			p.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil}))
			p.ToolsListResponseInterceptors = tc.interceptors

			_, err := postJSON(t, p, toolsListRequest)
			require.NoError(t, err)

			require.Equal(t, tc.wantLevel.String(), logLevelOf(t, logs.String(), tc.message))
		})
	}
}

// logLevelOf returns the level of the single JSON log record carrying msg.
func logLevelOf(t *testing.T, logs string, msg string) string {
	t.Helper()
	var levels []string
	for line := range strings.SplitSeq(strings.TrimSpace(logs), "\n") {
		var record struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record.Msg == msg {
			levels = append(levels, record.Level)
		}
	}
	require.Len(t, levels, 1, "want exactly one record for %q", msg)
	return levels[0]
}
