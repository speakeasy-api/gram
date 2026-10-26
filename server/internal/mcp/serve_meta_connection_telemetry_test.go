package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

// Gateway rows carry the connection's discovery mode and the MCP session the
// client named, so activity can order each session's calls.
func TestServePublic_MetaEndpoint_RowsCarryModeAndClientSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")
	sessionID := "client-session-" + uuid.NewString()

	for _, call := range []struct {
		tool      string
		arguments map[string]any
	}{
		{tool: metamcp.ToolListServers, arguments: map[string]any{}},
		{tool: metamcp.ToolExecuteTool, arguments: map[string]any{"name": member.slug + "--alpha_tool", "arguments": map[string]any{}}},
	} {
		w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name":      call.tool,
			"arguments": call.arguments,
		}), "", map[string]string{"Mcp-Session-Id": sessionID})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	}

	requireTelemetryRowCount(t, `gram_project_id = ? AND meta_mcp_server_id = ?
		   AND ((event_source = 'meta_discovery' AND tool_name = 'list_servers') OR (event_source = 'tool_call' AND tool_name = 'alpha_tool'))
		   AND toString(attributes.gram.meta_mcp_server.discovery_mode) = 'progressive'
		   AND toString(attributes.mcp.session.id) = ?`,
		2, authCtx.ProjectID.String(), meta.ID.String(), sessionID)
}

// A request that names no MCP session is served under a fresh one, which its
// rows leave out: each such call would otherwise open a session of its own.
func TestServePublic_MetaEndpoint_RowsOmitMintedSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	callMetaTool(t, ctx, ti, slug, metamcp.ToolListServers, map[string]any{})

	requireTelemetryRowCount(t, `gram_project_id = ? AND meta_mcp_server_id = ? AND tool_name = 'list_servers'
		   AND toString(attributes.gram.meta_mcp_server.discovery_mode) = 'progressive'
		   AND ifNull(toString(attributes.mcp.session.id), '') = ''`,
		1, authCtx.ProjectID.String(), meta.ID.String())
}

// Each Code Mode run records one execute row and one row per discovery helper
// it calls, all in code_mode.
func TestCodeModeRecordsHelperAndRunRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil, run: func(ctx context.Context, _ string, host codemode.Host) (*codemode.ExecutionResult, error) {
		_, err := host.Servers(ctx, codemode.PageArgs{Limit: 10, Cursor: ""})
		require.NoError(t, err)
		_, err = host.Search(ctx, codemode.SearchArgs{Query: "", Server: "", Limit: 10, Cursor: ""})
		require.NoError(t, err)
		_, err = host.Describe(ctx, fx.granted+"--missing_tool")
		require.Error(t, err)
		return &codemode.ExecutionResult{Value: json.RawMessage("1"), ToolCalls: []codemode.CallOutcome{}}, nil
	}}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	gatewayID := endpoint.MetaMcpServerID.UUID.String()

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolCodeExecute, "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	for _, row := range []struct {
		tool   string
		status int
	}{
		{tool: metamcp.ToolCodeServers, status: http.StatusOK},
		{tool: metamcp.ToolCodeSearch, status: http.StatusOK},
		{tool: metamcp.ToolCodeDescribe, status: http.StatusNotFound},
		{tool: metamcp.ToolCodeExecute, status: http.StatusOK},
	} {
		// The mock organization's gateway is shared with parallel tests; the
		// caller is this test's own.
		requireTelemetryRowCount(t, `meta_mcp_server_id = ? AND user_id = ? AND event_source = 'meta_discovery' AND tool_name = ?
			   AND gram_urn = ?
			   AND toInt32OrZero(toString(attributes.http.response.status_code)) = ?
			   AND toString(attributes.gram.meta_mcp_server.discovery_mode) = 'code_mode'`,
			1, gatewayID, fx.subject.ID, row.tool, "metamcp:"+gatewayID+":"+row.tool, row.status)
	}
}

// A run the runtime refused still records its execute row, with the refusal's
// status.
func TestCodeModeRecordsRefusedRun(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	mcp.SetTestCodeExecutor(ti.service.Service, &refusingCodeExecutor{err: codemode.ErrCapacity})
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolCodeExecute, "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	requireTelemetryRowCount(t, `meta_mcp_server_id = ? AND user_id = ? AND event_source = 'meta_discovery' AND tool_name = 'execute'
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) = 503
		   AND toString(attributes.gram.meta_mcp_server.discovery_mode) = 'code_mode'`,
		1, endpoint.MetaMcpServerID.UUID.String(), fx.subject.ID)
}
