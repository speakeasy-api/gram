package remotemcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newToolsListRequest(t *testing.T, sessionID string) *proxy.ToolsListRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", http.NoBody)
	require.NoError(t, err)
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	return &proxy.ToolsListRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: httpReq,
		},
		Params: &mcp.ListToolsParams{},
	}
}

func TestToolsListPostHogEventInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerIdentity, testenv.NewLogger(t))
	require.Equal(t, "tools-list-posthog-event", interceptor.Name())
}

func TestToolsListPostHogEventInterceptor_MissingRequestContextPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerIdentity, testenv.NewLogger(t))

	require.NoError(t, interceptor.InterceptToolsListRequest(t.Context(), newToolsListRequest(t, "session-1")))
}

func TestToolsListPostHogEventInterceptor_PassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerIdentity, testenv.NewLogger(t))

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-tools-list",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/" + testServerID,
	})

	require.NoError(t, interceptor.InterceptToolsListRequest(ctx, newToolsListRequest(t, "session-tools")))
}

// TestToolsListPostHogEventInterceptor_Version20260728RequestMetadataPassesThrough
// exercises the enrichment path: a 2026-07-28 request whose per-request
// `_meta` carries the protocol version, client identity, and capabilities the
// event records. The PostHog client is a no-op in tests, so the contract
// asserted is that enrichment never rejects the request.
func TestToolsListPostHogEventInterceptor_Version20260728RequestMetadataPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerIdentity, testenv.NewLogger(t))

	list := newToolsListRequest(t, "")
	list.UserRequest.UserHTTPRequest.Header.Set(mcpversions.HTTPHeader, mcpversions.Version20260728)
	list.UserRequest.JSONRPCMessages = []jsonrpc.Message{&jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/list",
		Params: json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"ExampleClient","version":"1.0.0"},"io.modelcontextprotocol/clientCapabilities":{"roots":{}}}}`),
		Extra:  nil,
	}}

	ctx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/" + testServerID,
	})

	require.NoError(t, interceptor.InterceptToolsListRequest(ctx, list))
}

func TestToolsListPostHogEventInterceptor_MissingSessionIDPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerIdentity, testenv.NewLogger(t))

	ctx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/" + testServerID,
	})

	require.NoError(t, interceptor.InterceptToolsListRequest(ctx, newToolsListRequest(t, "")))
}
