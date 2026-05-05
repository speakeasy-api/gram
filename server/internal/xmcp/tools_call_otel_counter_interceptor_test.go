package xmcp

import (
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newCounterInterceptorForTest builds an interceptor backed by a testenv OTel
// meter. The interceptor exercises the recording path; we cannot directly
// observe what dimensions the meter receives, so tests verify the
// interceptor never rejects (the only contract the rest of the pipeline
// depends on). This mirrors the assertion shape of /mcp's metrics tests.
func newCounterInterceptorForTest(t *testing.T) *ToolsCallOTELCounterInterceptor {
	t.Helper()
	logger := testenv.NewLogger(t)
	return NewToolsCallOTELCounterInterceptor(newMetrics(testenv.NewMeterProvider(t).Meter("test"), logger), logger)
}

func newToolsCallRequestForCounter(t *testing.T, toolName string) *proxy.ToolsCallRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", http.NoBody)
	require.NoError(t, err)

	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: httpReq,
		},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: nil,
			Meta:      nil,
		},
	}
}

func TestToolsCallOTELCounterInterceptor_Name(t *testing.T) {
	t.Parallel()

	require.Equal(t, "tools-call-otel-counter", newCounterInterceptorForTest(t).Name())
}

func TestToolsCallOTELCounterInterceptor_NoAuthContextPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := newCounterInterceptorForTest(t)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequestForCounter(t, "any_tool")))
}

func TestToolsCallOTELCounterInterceptor_AuthenticatedRequestPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := newCounterInterceptorForTest(t)

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-counter",
		AccountType:          string(billing.TierPro),
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/abc",
	})

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequestForCounter(t, "search_tickets")))
}

func TestToolsCallOTELCounterInterceptor_NeverRejects(t *testing.T) {
	t.Parallel()

	interceptor := newCounterInterceptorForTest(t)

	// Counter recording is best-effort: even with an empty auth context the
	// interceptor must return nil so the request is not blocked.
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{})

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequestForCounter(t, "any_tool")))
}
