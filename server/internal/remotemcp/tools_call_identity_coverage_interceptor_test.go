package remotemcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

type recordingIdentityCoverage struct {
	organizationID string
	surface        mcpmetrics.KillswitchCoverageSurface
	source         mcptoolexecution.ServerSource
	calls          int
}

func (r *recordingIdentityCoverage) Record(_ context.Context, organizationID string, surface mcpmetrics.KillswitchCoverageSurface, source mcptoolexecution.ServerSource) {
	r.organizationID = organizationID
	r.surface = surface
	r.source = source
	r.calls++
}

func TestToolsCallIdentityCoverageInterceptor_RecordsBeforeTypedParamsDecode(t *testing.T) {
	t.Parallel()

	serverID := uuid.New()
	coverage := &recordingIdentityCoverage{}
	interceptor := NewToolsCallIdentityCoverageInterceptor(coverage, proxy.ServerIdentity{McpServerID: serverID.String()}, "org-route")
	req := &proxy.UserRequest{JSONRPCMessages: []jsonrpc.Message{&jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: json.RawMessage(`"malformed params"`),
	}}}

	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), req))
	require.Equal(t, 1, coverage.calls)
	require.Equal(t, "org-route", coverage.organizationID)
	require.Equal(t, mcpmetrics.KillswitchSurfacePrivateProxy, coverage.surface)
	require.Equal(t, uuid.NullUUID{UUID: serverID, Valid: true}, coverage.source.FrontingServerID)
}

func TestToolsCallIdentityCoverageInterceptor_PreservesMalformedRouteIdentityAsPresent(t *testing.T) {
	t.Parallel()

	coverage := &recordingIdentityCoverage{}
	interceptor := NewToolsCallIdentityCoverageInterceptor(coverage, proxy.ServerIdentity{McpServerID: "not-a-uuid"}, "org-route")
	req := &proxy.UserRequest{JSONRPCMessages: []jsonrpc.Message{&jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: json.RawMessage(`{}`),
	}}}

	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), req))
	require.Equal(t, 1, coverage.calls)
	require.Equal(t, uuid.NullUUID{UUID: uuid.Nil, Valid: true}, coverage.source.FrontingServerID)
}

func TestToolsCallIdentityCoverageInterceptor_IgnoresOtherMethods(t *testing.T) {
	t.Parallel()

	coverage := &recordingIdentityCoverage{}
	interceptor := NewToolsCallIdentityCoverageInterceptor(coverage, proxy.ServerIdentity{}, "org-route")
	req := &proxy.UserRequest{JSONRPCMessages: []jsonrpc.Message{&jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/list",
		Params: json.RawMessage(`{}`),
	}}}

	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), req))
	require.Zero(t, coverage.calls)
}
