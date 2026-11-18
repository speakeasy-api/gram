package remotemcp_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const selectionTestGrantID = "0198a0b0-0000-7000-8000-0000000000aa"

// selectionOf builds a compiled selection whose frozen name grant is
// exactly the given names.
func selectionOf(t *testing.T, names ...string) *toolfilter.SessionSelection {
	t.Helper()
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, fmt.Sprintf(`{"type":"tool","name":%q}`, name))
	}
	allow := "[" + strings.Join(entries, ",") + "]"
	raw := fmt.Sprintf(`{"resource":"mcp_server:0198a0b0-0000-7000-8000-000000000001","grant_id":%q,"allow":%s}`, selectionTestGrantID, allow)
	sel, err := toolfilter.ParseSessionSelection([]byte(raw))
	require.NoError(t, err)
	return sel
}

// liveSelection builds a compiled selection granting the annotation live,
// plus optional frozen tool names.
func liveSelection(t *testing.T, annotation string, names ...string) *toolfilter.SessionSelection {
	t.Helper()
	entries := []string{fmt.Sprintf(`{"type":"annotation","name":%q,"mode":"live"}`, annotation)}
	for _, name := range names {
		entries = append(entries, fmt.Sprintf(`{"type":"tool","name":%q}`, name))
	}
	allow := "[" + strings.Join(entries, ",") + "]"
	raw := fmt.Sprintf(`{"resource":"mcp_server:0198a0b0-0000-7000-8000-000000000001","grant_id":%q,"allow":%s}`, selectionTestGrantID, allow)
	sel, err := toolfilter.ParseSessionSelection([]byte(raw))
	require.NoError(t, err)
	return sel
}

func namedTools(names ...string) []*mcp.Tool {
	tools := make([]*mcp.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, &mcp.Tool{Name: name})
	}
	return tools
}

func readOnlySDKTool(name string) *mcp.Tool {
	return &mcp.Tool{Name: name, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// withListSession stamps the client-facing MCP session id and request
// cursor onto a tools/list response so witnessing has an identity.
func withListSession(resp *proxy.ToolsListResponse, sessionID, requestCursor string) *proxy.ToolsListResponse {
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set(proxy.McpSessionIDHeader, sessionID)
	resp.RemoteMessage.UserHTTPRequest = req
	resp.Request = &proxy.ToolsListRequest{
		Params:      &mcp.ListToolsParams{Meta: nil, Cursor: requestCursor},
		UserRequest: nil,
	}
	return resp
}

func TestSessionSelectionInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "a"), nil)
	require.Equal(t, "session-selection", interceptor.Name())
}

func TestSessionSelectionInterceptor_ListFiltersToSelectedSubsetInUpstreamOrder(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "delta", "alpha"), nil)
	resp := newToolsListResponse(t, namedTools("alpha", "bravo", "charlie", "delta"))

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Equal(t, []string{"alpha", "delta"}, toolNames(resp.Result.Tools))
}

func TestSessionSelectionInterceptor_ListEmptySelectionFiltersEverything(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t), nil)
	resp := newToolsListResponse(t, namedTools("alpha", "bravo"))

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Empty(t, resp.Result.Tools)
	require.NotNil(t, resp.Result.Tools)
}

func TestSessionSelectionInterceptor_ListLiveGrantMatchesHintedTools(t *testing.T) {
	t.Parallel()

	store := toolfilter.NewSessionToolWitnessStore(testenv.NewLogger(t), testenv.NewMemoryCache())
	interceptor := remotemcp.NewSessionSelectionInterceptor(liveSelection(t, toolfilter.AnnotationReadOnly), store)
	resp := withListSession(newToolsListResponse(t, []*mcp.Tool{
		readOnlySDKTool("reader"),
		{Name: "writer"},
	}), "sess-live", "")

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Equal(t, []string{"reader"}, toolNames(resp.Result.Tools))
}

func TestSessionSelectionInterceptor_ListErrorResponsePassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	resp := &proxy.ToolsListResponse{
		Error: &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil},
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: nil,
			Message:            &jsonrpc.Response{ID: jsonrpc.ID{}, Result: nil, Error: nil},
		},
		Request: nil,
		Result:  nil,
	}

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}

func TestSessionSelectionInterceptor_ListNilResponseFailsClosed(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	require.Error(t, interceptor.InterceptToolsListResponse(t.Context(), nil))
}

func TestSessionSelectionInterceptor_ListMissingResultAndErrorFailsClosed(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	resp := &proxy.ToolsListResponse{
		Error:         nil,
		RemoteMessage: nil,
		Request:       nil,
		Result:        nil,
	}
	require.Error(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}

func TestSessionSelectionInterceptor_ListNilSelectionFailsClosedToZeroTools(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(nil, nil)
	resp := newToolsListResponse(t, namedTools("alpha"))

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Empty(t, resp.Result.Tools)
}

func TestSessionSelectionInterceptor_SessionlessListWitnessAuthorizesCall(t *testing.T) {
	t.Parallel()

	// Stateless upstreams omit the MCP session id. Their witness is scoped by
	// the consent grant and must support the same list-to-call round trip.
	store := toolfilter.NewSessionToolWitnessStore(testenv.NewLogger(t), testenv.NewMemoryCache())
	interceptor := remotemcp.NewSessionSelectionInterceptor(liveSelection(t, toolfilter.AnnotationReadOnly, "writer"), store)
	resp := newToolsListResponse(t, []*mcp.Tool{
		readOnlySDKTool("reader"),
		{Name: "writer"},
	})

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Equal(t, []string{"reader", "writer"}, toolNames(resp.Result.Tools))
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequest("reader")))
	require.Error(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestWithSession("reader", "sess-1")), "stateless witness must not leak into a stateful session")
}

// newToolsCallRequestWithSession builds a tools/call view carrying the
// client-facing MCP session id header the witness store keys on.
func newToolsCallRequestWithSession(toolName, sessionID string) *proxy.ToolsCallRequest {
	call := newToolsCallRequest(toolName)
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set(proxy.McpSessionIDHeader, sessionID)
	call.UserRequest = &proxy.UserRequest{UserHTTPRequest: req, JSONRPCMessages: nil}
	return call
}

func TestSessionSelectionInterceptor_CallAllowsSelectedTool(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha", "bravo"), nil)
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequest("alpha")))
}

func TestSessionSelectionInterceptor_CallRejectsUnselectedToolWithTypedRejection(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	err := interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequest("charlie"))
	require.Error(t, err)

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject)
	require.Equal(t, proxy.RejectCodeInvalidRequest, reject.Code)
	require.Equal(t, "tool is not approved for this session", reject.Message)
}

func TestSessionSelectionInterceptor_CallEmptySelectionRejectsEverything(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t), nil)
	require.Error(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequest("alpha")))
}

func TestSessionSelectionInterceptor_CallNilCallFailsClosed(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	require.Error(t, interceptor.InterceptToolsCallRequest(t.Context(), nil))
}

func TestSessionSelectionInterceptor_CallNilParamsFailsClosed(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewSessionSelectionInterceptor(selectionOf(t, "alpha"), nil)
	call := &proxy.ToolsCallRequest{Params: nil, UserRequest: nil}
	require.Error(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
}

// TestSessionSelection_ListWitnessAuthorizesLiveCall is the listing-witnessed
// round trip: a live-granted tool relayed by the list interceptor becomes
// callable for the same grant and session, and only there.
func TestSessionSelection_ListWitnessAuthorizesLiveCall(t *testing.T) {
	t.Parallel()

	selection := liveSelection(t, toolfilter.AnnotationReadOnly)
	store := toolfilter.NewSessionToolWitnessStore(testenv.NewLogger(t), testenv.NewMemoryCache())
	listInterceptor := remotemcp.NewSessionSelectionInterceptor(selection, store)
	callInterceptor := remotemcp.NewSessionSelectionInterceptor(selection, store)

	// Cold witness: the live-granted tool is not yet callable.
	require.Error(t, callInterceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestWithSession("reader", "sess-1")))

	resp := withListSession(newToolsListResponse(t, []*mcp.Tool{readOnlySDKTool("reader"), {Name: "writer"}}), "sess-1", "")
	require.NoError(t, listInterceptor.InterceptToolsListResponse(t.Context(), resp))

	require.NoError(t, callInterceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestWithSession("reader", "sess-1")))
	require.Error(t, callInterceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestWithSession("writer", "sess-1")), "unmatched listed tool stays out")
	require.Error(t, callInterceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestWithSession("reader", "other-sess")), "witness is session-scoped")
}
