package remotemcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newResourcesReadResponseForInterceptor(t *testing.T, sessionID string, resourceURI string) *proxy.ResourcesReadResponse {
	t.Helper()

	rpcReq, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"` + resourceURI + `"}}`))
	require.NoError(t, err)
	req, ok := rpcReq.(*jsonrpc.Request)
	require.True(t, ok)

	rpcResp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"contents":[{"uri":"` + resourceURI + `","text":"hello"}]}}`))
	require.NoError(t, err)
	resp, ok := rpcResp.(*jsonrpc.Response)
	require.True(t, ok)

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server-id", http.NoBody)
	require.NoError(t, err)
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	return &proxy.ResourcesReadResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    httpReq,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
			Message:            resp,
		},
		Request: &proxy.ResourcesReadRequest{
			UserRequest: &proxy.UserRequest{
				UserHTTPRequest: httpReq,
				JSONRPCMessages: []jsonrpc.Message{req},
			},
			Params: &mcp.ReadResourceParams{
				Meta: nil,
				URI:  resourceURI,
			},
		},
		Result: &mcp.ReadResourceResult{
			Meta:     nil,
			Contents: nil,
		},
	}
}

func TestResourcesReadUsageTrackingInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(newFakeBillingTracker(), testenv.NewLogger(t))
	require.Equal(t, "resources-read-usage-tracking", interceptor.Name())
}

func TestResourcesReadUsageTrackingInterceptor_NoAuthContextSkips(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	read := newResourcesReadResponseForInterceptor(t, "", "file:///etc/hosts")

	require.NoError(t, interceptor.InterceptResourcesReadResponse(t.Context(), read))

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	require.Empty(t, tracker.events, "missing auth context must short-circuit before tracking")
}

func TestResourcesReadUsageTrackingInterceptor_EmitsEventForBaseTier(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	projectID := uuid.New()
	projectSlug := "demo-project"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
		OrganizationSlug:     "demo-org",
		ProjectID:            &projectID,
		ProjectSlug:          &projectSlug,
	})

	read := newResourcesReadResponseForInterceptor(t, "session-abc", "file:///etc/hosts")

	require.NoError(t, interceptor.InterceptResourcesReadResponse(ctx, read))

	event := tracker.waitForEvent(t)
	require.Equal(t, "org-free", event.OrganizationID)
	require.Empty(t, event.ToolName, "ToolName is empty for resource reads — only ResourceURI is meaningful")
	require.Equal(t, "file:///etc/hosts", event.ResourceURI)
	require.Equal(t, projectID.String(), event.ProjectID)
	require.NotNil(t, event.ProjectSlug)
	require.Equal(t, projectSlug, *event.ProjectSlug)
	require.NotNil(t, event.OrganizationSlug)
	require.Equal(t, "demo-org", *event.OrganizationSlug)
	require.Equal(t, billing.ToolCallTypeExternalMCP, event.Type)
	require.Equal(t, http.StatusOK, event.ResponseStatusCode)
	require.NotNil(t, event.MCPSessionID)
	require.Equal(t, "session-abc", *event.MCPSessionID)
	require.Positive(t, event.RequestBytes)
	require.Positive(t, event.OutputBytes)
}

func TestResourcesReadUsageTrackingInterceptor_EmitsEventForPaidTier(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-pro",
		AccountType:          string(billing.TierPro),
		ProjectID:            &projectID,
	})

	read := newResourcesReadResponseForInterceptor(t, "", "https://example.com/data")

	require.NoError(t, interceptor.InterceptResourcesReadResponse(ctx, read))

	event := tracker.waitForEvent(t)
	require.Equal(t, "org-pro", event.OrganizationID)
	require.Equal(t, "https://example.com/data", event.ResourceURI)
	require.Equal(t, billing.ToolCallTypeExternalMCP, event.Type)
	require.Nil(t, event.MCPSessionID, "absent Mcp-Session-Id header must not produce an empty-string pointer")
}

func TestResourcesReadUsageTrackingInterceptor_EmptyJSONRPCMessagesDoesNotPanic(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-pro",
		AccountType:          string(billing.TierPro),
		ProjectID:            &projectID,
	})

	// The proxy's typed-view constructor never produces an empty
	// JSONRPCMessages slice, but the interceptor is publicly constructible
	// — assert it tolerates the edge case rather than panicking on [0].
	read := newResourcesReadResponseForInterceptor(t, "", "file:///etc/hosts")
	read.Request.UserRequest.JSONRPCMessages = nil

	require.NoError(t, interceptor.InterceptResourcesReadResponse(ctx, read))

	event := tracker.waitForEvent(t)
	require.Zero(t, event.RequestBytes, "missing JSON-RPC request must zero RequestBytes, not panic")
	require.Equal(t, "file:///etc/hosts", event.ResourceURI)
}
