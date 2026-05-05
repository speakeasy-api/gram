package xmcp_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// fakeBillingTracker implements [billing.Tracker]. Only TrackToolCallUsage is
// exercised; other methods record a no-op so accidental use is visible. The
// captured event is published on a channel so tests can synchronously wait
// for the asynchronous track goroutine.
type fakeBillingTracker struct {
	mu     sync.Mutex
	events []billing.ToolCallUsageEvent
	done   chan struct{}
}

func newFakeBillingTracker() *fakeBillingTracker {
	return &fakeBillingTracker{done: make(chan struct{}, 1)}
}

func (f *fakeBillingTracker) TrackToolCallUsage(_ context.Context, event billing.ToolCallUsageEvent) {
	f.mu.Lock()
	f.events = append(f.events, event)
	f.mu.Unlock()
	select {
	case f.done <- struct{}{}:
	default:
	}
}

func (f *fakeBillingTracker) TrackPromptCallUsage(_ context.Context, _ billing.PromptCallUsageEvent) {
}
func (f *fakeBillingTracker) TrackModelUsage(_ context.Context, _ billing.ModelUsageEvent) {}
func (f *fakeBillingTracker) TrackPlatformUsage(_ context.Context, _ []billing.PlatformUsageEvent) {
}

func (f *fakeBillingTracker) waitForEvent(t *testing.T) billing.ToolCallUsageEvent {
	t.Helper()
	select {
	case <-f.done:
	case <-t.Context().Done():
		t.Fatal("tracker goroutine did not fire before test context cancellation")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.events, 1)
	return f.events[0]
}

func newToolsCallResponseForInterceptor(t *testing.T, sessionID string) *proxy.ToolsCallResponse {
	t.Helper()

	rpcResp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	require.NoError(t, err)
	resp, ok := rpcResp.(*jsonrpc.Response)
	require.True(t, ok)

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server-id", http.NoBody)
	require.NoError(t, err)
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	return &proxy.ToolsCallResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    httpReq,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: &http.Response{StatusCode: http.StatusOK},
			Message:            resp,
		},
		Request: &proxy.ToolsCallRequest{
			UserRequest: nil,
			Params: &mcp.CallToolParamsRaw{
				Arguments: []byte(`{"foo":"bar"}`),
				Meta:      nil,
				Name:      "search_tickets",
			},
		},
		Result: &mcp.CallToolResult{
			Content:           nil,
			IsError:           false,
			Meta:              nil,
			StructuredContent: nil,
		},
	}
}

func TestToolsCallUsageTrackingInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsCallUsageTrackingInterceptor(newFakeBillingTracker(), testenv.NewLogger(t))
	require.Equal(t, "tools-call-usage-tracking", interceptor.Name())
}

func TestToolsCallUsageTrackingInterceptor_NoAuthContextSkips(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := xmcp.NewToolsCallUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	call := newToolsCallResponseForInterceptor(t, "")

	require.NoError(t, interceptor.InterceptToolsCallResponse(t.Context(), call))

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	require.Empty(t, tracker.events, "missing auth context must short-circuit before tracking")
}

func TestToolsCallUsageTrackingInterceptor_EmitsEventForBaseTier(t *testing.T) {
	t.Parallel()

	tracker := newFakeBillingTracker()
	interceptor := xmcp.NewToolsCallUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	projectID := uuid.New()
	projectSlug := "demo-project"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
		OrganizationSlug:     "demo-org",
		ProjectID:            &projectID,
		ProjectSlug:          &projectSlug,
	})

	call := newToolsCallResponseForInterceptor(t, "session-abc")

	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, call))

	event := tracker.waitForEvent(t)
	require.Equal(t, "org-free", event.OrganizationID)
	require.Equal(t, "search_tickets", event.ToolName)
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

func TestToolsCallUsageTrackingInterceptor_EmitsEventForPaidTier(t *testing.T) {
	t.Parallel()

	// Tracking must fire regardless of tier so Polar metering matches /mcp.
	// Limits gating is a separate concern handled by the request-side
	// interceptor.
	tracker := newFakeBillingTracker()
	interceptor := xmcp.NewToolsCallUsageTrackingInterceptor(tracker, testenv.NewLogger(t))

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-pro",
		AccountType:          string(billing.TierPro),
		ProjectID:            &projectID,
	})

	call := newToolsCallResponseForInterceptor(t, "")

	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, call))

	event := tracker.waitForEvent(t)
	require.Equal(t, "org-pro", event.OrganizationID)
	require.Equal(t, billing.ToolCallTypeExternalMCP, event.Type)
	require.Nil(t, event.MCPSessionID, "absent Mcp-Session-Id header must not produce an empty-string pointer")
}
