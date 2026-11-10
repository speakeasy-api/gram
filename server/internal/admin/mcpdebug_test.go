package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newMCPDebugTestService(t *testing.T, policy *guardian.Policy) *Service {
	t.Helper()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-debug", "operator@example.com")))
	if policy != nil {
		svc.SetMCPDebugPolicy(policy)
	}
	return svc
}

func requireMCPDebugOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func TestDebugMcpURL_UnavailableWithoutPolicy(t *testing.T) {
	t.Parallel()
	svc := newMCPDebugTestService(t, nil)

	_, err := svc.DebugMcpURL(t.Context(), &gen.DebugMcpURLPayload{AdminSessionToken: nil, URL: "https://mcp.example.com/mcp"})
	requireMCPDebugOopsCode(t, err, oops.CodeUnavailable)
}

func TestDebugMcpURL_RejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	svc := newMCPDebugTestService(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))

	_, err := svc.DebugMcpURL(t.Context(), &gen.DebugMcpURLPayload{AdminSessionToken: nil, URL: "ftp://mcp.example.com/mcp"})
	requireMCPDebugOopsCode(t, err, oops.CodeBadRequest)
}

func TestDebugMcpURL_BlockedHostIsUnreachableTrace(t *testing.T) {
	t.Parallel()
	svc := newMCPDebugTestService(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))

	report, err := svc.DebugMcpURL(t.Context(), &gen.DebugMcpURLPayload{AdminSessionToken: nil, URL: "https://127.0.0.1/mcp"})
	require.NoError(t, err)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, string(report.Outcome))
	require.Len(t, report.Steps, 7)
	require.Equal(t, remotemcp.DebugStatusFailed, string(report.Steps[0].Status))
}

func TestDebugMcpURL_ListsAnOpenServer(t *testing.T) {
	t.Parallel()
	server := mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_things", Description: "Lists things."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	svc := newMCPDebugTestService(t, policy)

	report, err := svc.DebugMcpURL(t.Context(), &gen.DebugMcpURLPayload{AdminSessionToken: nil, URL: upstream.URL})
	require.NoError(t, err)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, string(report.Outcome))
	require.Equal(t, remotemcp.DebugAuthMethodNone, string(report.AuthMethod))
	require.Len(t, report.Tools, 1)
	require.Equal(t, "list_things", report.Tools[0].Name)
	require.NotNil(t, report.ServerName)
	require.Equal(t, "upstream", *report.ServerName)
}
