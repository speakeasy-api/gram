package xmcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
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

	interceptor := xmcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerID, testenv.NewLogger(t))
	require.Equal(t, "tools-list-posthog-event", interceptor.Name())
}

func TestToolsListPostHogEventInterceptor_MissingRequestContextPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerID, testenv.NewLogger(t))

	require.NoError(t, interceptor.InterceptToolsListRequest(t.Context(), newToolsListRequest(t, "session-1")))
}

func TestToolsListPostHogEventInterceptor_PassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerID, testenv.NewLogger(t))

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

func TestToolsListPostHogEventInterceptor_MissingSessionIDPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListPostHogEventInterceptor(newPosthogForTest(t), testServerID, testenv.NewLogger(t))

	ctx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/" + testServerID,
	})

	require.NoError(t, interceptor.InterceptToolsListRequest(ctx, newToolsListRequest(t, "")))
}
