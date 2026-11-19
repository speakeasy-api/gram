package proxy_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const localToolName = "submit_feedback"

// remoteToolsListResult is a remote server's tools/list result with members
// the SDK's tool type does not model, on the result and on a tool.
const remoteToolsListResult = `{"jsonrpc":"2.0","id":2,"result":{"resultType":"complete","ttlMs":30000,"tools":[` +
	`{"name":"create_task","description":"Create a task","inputSchema":{"type":"object"},"execution":{"taskSupport":"optional"},"x-upstream":{"big":9007199254740993}},` +
	`{"name":"list_tasks","description":"List tasks","inputSchema":{"type":"object"}}` +
	`]}}`

func localToolsListRequest(cursor string) string {
	if cursor == "" {
		return `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	}
	return `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":"` + cursor + `"}}`
}

func localToolsCallRequest(name string) string {
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + name + `","arguments":{"content":"it broke"}}}`
}

// localTool is a tool the proxy answers itself. calls counts how often it ran.
func localTool(calls *atomic.Int32, callErr error) proxy.LocalTool {
	return proxy.LocalTool{
		Definition: &mcp.Tool{
			Meta:         nil,
			Annotations:  nil,
			Description:  "Tell the team what went wrong.",
			InputSchema:  json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`),
			Name:         localToolName,
			OutputSchema: nil,
			Title:        "Submit feedback",
			Icons:        nil,
		},
		Call: func(_ context.Context, call *proxy.ToolsCallRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			if callErr != nil {
				return nil, callErr
			}
			return &mcp.CallToolResult{
				Meta:              nil,
				Content:           []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: "received " + string(call.Params.Arguments)}},
				StructuredContent: json.RawMessage(`{"status":"received"}`),
				IsError:           false,
				InputRequests:     nil,
				RequestState:      "",
			}, nil
		},
	}
}

// countingRefusalInterceptor counts the tools/call refusals the proxy reports.
type countingRefusalInterceptor struct {
	refusals *atomic.Int32
}

func (countingRefusalInterceptor) Name() string { return "refusals" }

func (c countingRefusalInterceptor) InterceptToolsCallRefusal(context.Context, error) {
	c.refusals.Add(1)
}

func resolveLocalTools(resolutions *atomic.Int32, tools ...proxy.LocalTool) proxy.LocalToolsResolver {
	return func(context.Context) ([]proxy.LocalTool, error) {
		resolutions.Add(1)
		return tools, nil
	}
}

// remoteServer answers every request with body and counts the requests it saw.
func remoteServer(t *testing.T, contentType, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &requests
}

func postToProxy(t *testing.T, p *proxy.Proxy, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	return rr
}

// listedTools decodes a tools/list response into its tools, each as the bytes
// it was relayed in.
func listedTools(t *testing.T, body []byte) (names []string, tools map[string]json.RawMessage, result map[string]json.RawMessage) {
	t.Helper()

	var response struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	var raw []json.RawMessage
	require.NoError(t, json.Unmarshal(response.Result["tools"], &raw))

	tools = make(map[string]json.RawMessage, len(raw))
	for _, tool := range raw {
		var named struct {
			Name string `json:"name"`
		}
		require.NoError(t, json.Unmarshal(tool, &named))
		names = append(names, named.Name)
		tools[named.Name] = tool
	}
	return names, tools, response.Result
}

func TestLocalTool_IsListedAfterTheRemoteServersTools(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", remoteToolsListResult)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	rr := postToProxy(t, p, localToolsListRequest(""))
	require.Equal(t, http.StatusOK, rr.Code)

	names, tools, result := listedTools(t, rr.Body.Bytes())
	require.Equal(t, []string{"create_task", "list_tasks", localToolName}, names)
	require.JSONEq(t, `{"name":"submit_feedback","title":"Submit feedback","description":"Tell the team what went wrong.","inputSchema":{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}}`, string(tools[localToolName]))

	// The remote server's tools and result keep members the SDK does not model.
	require.JSONEq(t, `{"name":"create_task","description":"Create a task","inputSchema":{"type":"object"},"execution":{"taskSupport":"optional"},"x-upstream":{"big":9007199254740993}}`, string(tools["create_task"]))
	require.Contains(t, string(tools["create_task"]), "9007199254740993", "a large number keeps its digits")
	require.JSONEq(t, `"complete"`, string(result["resultType"]))
	require.JSONEq(t, `30000`, string(result["ttlMs"]))

	require.Equal(t, int32(1), resolutions.Load(), "one request resolves the local tools once")
	require.Zero(t, calls.Load())
}

func TestLocalTool_TakesItsNameFromARemoteTool(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", `{"jsonrpc":"2.0","id":2,"result":{"tools":[`+
		`{"name":"submit_feedback","description":"The remote server's own","inputSchema":{"type":"object"}},`+
		`{"name":"list_tasks","description":"List tasks","inputSchema":{"type":"object"}}]}}`)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	names, tools, _ := listedTools(t, postToProxy(t, p, localToolsListRequest("")).Body.Bytes())
	require.Equal(t, []string{"list_tasks", localToolName}, names)
	require.Contains(t, string(tools[localToolName]), "Tell the team what went wrong.")
}

// A paginating client sees each local tool once, and never a remote tool that
// shares its name.
func TestLocalTool_IsListedOnTheFirstPageOnly(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", `{"jsonrpc":"2.0","id":2,"result":{"tools":[`+
		`{"name":"submit_feedback","description":"The remote server's own","inputSchema":{"type":"object"}},`+
		`{"name":"archive_task","description":"Archive a task","inputSchema":{"type":"object"}}]}}`)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	names, _, _ := listedTools(t, postToProxy(t, p, localToolsListRequest("page-2")).Body.Bytes())
	require.Equal(t, []string{"archive_task"}, names)
}

// A later page with nothing to take out lists exactly what the remote server sent.
func TestLocalTool_LeavesALaterPageWithoutItsNameAlone(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", remoteToolsListResult)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	names, tools, _ := listedTools(t, postToProxy(t, p, localToolsListRequest("page-2")).Body.Bytes())
	require.Equal(t, []string{"create_task", "list_tasks"}, names)
	require.Contains(t, string(tools["create_task"]), `"execution"`)
}

// The policies that narrow the remote server's tools do not apply to a local one.
func TestLocalTool_IsListedAfterInterceptorsFilterTheRemoteTools(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", remoteToolsListResult)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mutatingToolsListResponseInterceptor{name: "drop-everything", toolsFn: func([]*mcp.Tool) []*mcp.Tool { return nil }, err: nil},
	}

	names, _, _ := listedTools(t, postToProxy(t, p, localToolsListRequest("")).Body.Bytes())
	require.Equal(t, []string{localToolName}, names)
}

func TestLocalTool_IsListedOnAStreamedResponse(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "text/event-stream", "event: message\ndata: "+remoteToolsListResult+"\n\n")
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	out := postToProxy(t, p, localToolsListRequest("")).Body.String()
	var data string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "data: "); ok {
			data = rest
		}
	}
	require.NotEmpty(t, data, out)

	names, tools, _ := listedTools(t, []byte(data))
	require.Equal(t, []string{"create_task", "list_tasks", localToolName}, names)
	require.Contains(t, string(tools["create_task"]), `"execution"`)
}

func TestLocalTool_LeavesAnErrorResponseAlone(t *testing.T) {
	t.Parallel()

	const refused = `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"Method not found"}}`
	upstream, _ := remoteServer(t, "application/json", refused)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	require.JSONEq(t, refused, postToProxy(t, p, localToolsListRequest("")).Body.String())
}

func TestLocalTool_CallIsAnsweredWithoutTheRemoteServer(t *testing.T) {
	t.Parallel()

	upstream, requests := remoteServer(t, "application/json", `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"from the remote server"}]}}`)
	var calls, resolutions atomic.Int32
	var preForward, typed int
	var refused atomic.Int32
	failures := &mockToolsCallFailureInterceptor{failures: nil}
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))
	p.ToolsCallPreForwardInterceptors = []proxy.ToolsCallRequestInterceptor{&mockToolsCallRequestInterceptor{name: "kill-switch", called: &preForward, order: nil, err: nil, lastCall: nil}}
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{&mockToolsCallRequestInterceptor{name: "billing", called: &typed, order: nil, err: nil, lastCall: nil}}
	p.ToolsCallFailureInterceptors = []proxy.ToolsCallFailureInterceptor{failures}
	p.ToolsCallRefusalInterceptors = []proxy.ToolsCallRefusalInterceptor{countingRefusalInterceptor{refusals: &refused}}

	rr := postToProxy(t, p, localToolsCallRequest(localToolName))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.JSONEq(t, `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"received {\"content\":\"it broke\"}"}],"structuredContent":{"status":"received"}}}`, rr.Body.String())

	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, requests.Load(), "the remote server never sees a local tool's call")
	require.Equal(t, 1, preForward, "the checkpoint that can pause every tool call still runs")
	require.Zero(t, typed, "the remote server's per-tool policies do not run for a local tool")
	require.Empty(t, failures.failures, "a local answer is not a failed call")
	require.Zero(t, refused.Load(), "nor a refused one")
}

func TestLocalTool_OtherCallsReachTheRemoteServer(t *testing.T) {
	t.Parallel()

	upstream, requests := remoteServer(t, "application/json", `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"from the remote server"}]}}`)
	var calls, resolutions atomic.Int32
	var typed int
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))
	p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{&mockToolsCallRequestInterceptor{name: "billing", called: &typed, order: nil, err: nil, lastCall: nil}}

	rr := postToProxy(t, p, localToolsCallRequest("create_task"))
	require.Contains(t, rr.Body.String(), "from the remote server")
	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, 1, typed)
	require.Zero(t, calls.Load())
}

func TestLocalTool_ACallItRefusesIsAnsweredAsARejection(t *testing.T) {
	t.Parallel()

	upstream, requests := remoteServer(t, "application/json", `{}`)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, &proxy.RejectError{Code: proxy.RejectCodeInvalidParams, Message: "content is required", Data: nil}))

	rr := postToProxy(t, p, localToolsCallRequest(localToolName))
	require.JSONEq(t, `{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"content is required","data":null}}`, rr.Body.String())
	require.Zero(t, requests.Load())
}

// A call nobody waits on has no reply to put a result in, so it is forwarded
// like any other.
func TestLocalTool_ACallWithoutAnIDIsForwarded(t *testing.T) {
	t.Parallel()

	upstream, requests := remoteServer(t, "application/json", ``)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	postToProxy(t, p, `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"`+localToolName+`","arguments":{}}}`)
	require.Equal(t, int32(1), requests.Load())
	require.Zero(t, calls.Load())
}

// The remote server's own tools stay reachable when the local ones cannot be loaded.
func TestLocalTool_AFailedLookupServesTheRemoteServerAlone(t *testing.T) {
	t.Parallel()

	upstream, requests := remoteServer(t, "application/json", remoteToolsListResult)
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = func(context.Context) ([]proxy.LocalTool, error) { return nil, errors.New("database is down") }

	names, _, _ := listedTools(t, postToProxy(t, p, localToolsListRequest("")).Body.Bytes())
	require.Equal(t, []string{"create_task", "list_tasks"}, names)

	postToProxy(t, p, localToolsCallRequest(localToolName))
	require.Equal(t, int32(2), requests.Load(), "the call is forwarded, where the remote server answers for it")
}

// Only requests that list or call tools pay for resolving the local ones.
func TestLocalTool_IsNotResolvedForOtherRequests(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
	var calls, resolutions atomic.Int32
	p := newProxyForTest(t, upstream.URL)
	p.LocalTools = resolveLocalTools(&resolutions, localTool(&calls, nil))

	postToProxy(t, p, initializeRequest)
	postToProxy(t, p, `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`)
	postToProxy(t, p, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	require.Zero(t, resolutions.Load())
}

// A server that offers no local tool lists exactly what the remote server
// sent, the same as a proxy that was never given a resolver.
func TestLocalTool_NoneOfferedListsTheRemoteToolsAsTheyAre(t *testing.T) {
	t.Parallel()

	upstream, _ := remoteServer(t, "application/json", remoteToolsListResult)
	var resolutions atomic.Int32
	offersNone := newProxyForTest(t, upstream.URL)
	offersNone.LocalTools = resolveLocalTools(&resolutions)
	neverAsked := newProxyForTest(t, upstream.URL)

	require.Equal(t,
		postToProxy(t, neverAsked, localToolsListRequest("")).Body.String(),
		postToProxy(t, offersNone, localToolsListRequest("")).Body.String(),
	)
	require.Equal(t, int32(1), resolutions.Load())
}
