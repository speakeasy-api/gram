package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
)

// A gateway discovery call writes one meta_discovery row attributed to the gateway.
func TestServePublic_MetaEndpoint_DiscoveryEmitsTelemetryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedListedMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
	require.NotNil(t, envelope["result"])

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name = 'list_servers'
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) = 200`,
		1, authCtx.ProjectID.String(), meta.ID.String())

	// startsWith(gram_urn, 'tools:') is the query layer's tool-call classifier.
	requireTelemetryRowCount(t, `gram_project_id = ? AND meta_mcp_server_id = ?
		   AND event_source = 'meta_discovery' AND startsWith(gram_urn, 'tools:')`,
		0, authCtx.ProjectID.String(), meta.ID.String())
}

// A gateway execute_tool produces exactly one tool_call row, the member's,
// stamped with the gateway id.
func TestServePublic_MetaEndpoint_ExecuteStampsGatewayOnMemberRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	memberSlug := "remote-" + uuid.NewString()[:8]
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", memberSlug, 1, newRecordingUpstream(t, "alpha_tool").url)

	callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      memberSlug + "--alpha_tool",
		"arguments": map[string]any{},
	})

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'alpha_tool'
		   AND meta_mcp_server_id = ?
		   AND mcp_server_id = ?`,
		1, authCtx.ProjectID.String(), meta.ID.String(), memberID.String())

	requireTelemetryRowCount(t, `gram_project_id = ? AND event_source = 'tool_call' AND tool_name = 'alpha_tool'`,
		1, authCtx.ProjectID.String())
}

// A gateway execute_tool naming a tool its member does not serve leaves one
// failed tool_call row in the member's project, stamped with the gateway and
// the member.
func TestServePublic_MetaEndpoint_ExecuteUnknownToolLogsFailedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	memberSlug := "remote-" + uuid.NewString()[:8]
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", memberSlug, 1, newRecordingUpstream(t, "alpha_tool").url)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      memberSlug + "--missing_tool",
		"arguments": map[string]any{},
	})
	_, isError := metaToolResultText(t, envelope)
	require.True(t, isError)

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'missing_tool'
		   AND startsWith(gram_urn, 'tools:')
		   AND meta_mcp_server_id = ?
		   AND mcp_server_id = ?
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) = 500`,
		1, authCtx.ProjectID.String(), meta.ID.String(), memberID.String())
}

// A gateway client that sends no Mcp-Session-Id is handed the one the
// handshake was recorded under, and the tool calls it makes on that session
// carry the client it reported at initialize.
//
// Without the response header every request mints a fresh session id, the
// handshake record becomes unreachable, and gateway traffic reads as
// unattributed no matter what the client reported.
func TestServePublic_MetaEndpoint_InitializeSessionCarriesClientToToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	memberSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", memberSlug, 1, newRecordingUpstream(t, "alpha_tool").url)

	initialize, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "Claude Code", "version": "2.4.1"},
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initialize.Code, "body=%s", initialize.Body.String())

	sessionID := initialize.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID, "initialize must hand back the session it recorded the handshake under")

	execute, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": memberSlug + "--alpha_tool", "arguments": map[string]any{}},
	}), "", map[string]string{"Mcp-Session-Id": sessionID})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, execute.Code, "body=%s", execute.Body.String())

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'alpha_tool'
		   AND meta_mcp_server_id = ?
		   AND mcp_client_name = 'Claude Code'
		   AND mcp_client_version = '2.4.1'`,
		1, authCtx.ProjectID.String(), meta.ID.String())
}

// A describe_server whose member cannot be reached is answered as an isError
// result. The gateway's discovery row records that failure instead of a
// success, and the member gets one failed tool_call row with the reason, so
// the member's failure counts include the call. That row is not stamped with
// the gateway, which counts the call from its discovery row.
func TestServePublic_MetaEndpoint_DescribeServerMemberFailureLogsFailedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	f.down.Store(true)
	memberID := mcpServerBySlug(t, ctx, ti, f.project, "member")

	resp, err := servePublicHTTP(t, ctx, ti, f.metaSlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": "member"},
	}), f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.True(t, isError)

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name = 'describe_server'
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400
		   AND toString(attributes.error.message) = ?`,
		1, f.project.String(), text)
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'gateway_describe_server'
		   AND startsWith(gram_urn, 'tools:externalmcp:')
		   AND endsWith(gram_urn, ':gateway_describe_server')
		   AND tool_source != ''
		   AND mcp_server_id = ?
		   AND meta_mcp_server_id = ''
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400
		   AND toString(attributes.error.message) = ?`,
		1, f.project.String(), memberID.String(), text)
}

// A describe_tools that names a member it cannot reach still answers, listing
// the member under failed, and the member gets one failed tool_call row.
func TestServePublic_MetaEndpoint_DescribeToolsMemberFailureLogsFailedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	f.down.Store(true)
	memberID := mcpServerBySlug(t, ctx, ti, f.project, "member")

	resp, err := servePublicHTTP(t, ctx, ti, f.metaSlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeTools,
		"arguments": map[string]any{"tools": []string{"member--ping"}},
	}), f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.False(t, isError)
	require.Contains(t, text, `"failed"`)

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'gateway_describe_tools'
		   AND startsWith(gram_urn, 'tools:externalmcp:')
		   AND endsWith(gram_urn, ':gateway_describe_tools')
		   AND tool_source != ''
		   AND mcp_server_id = ?
		   AND meta_mcp_server_id = ''
		   AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400
		   AND toString(attributes.error.message) != ''`,
		1, f.project.String(), memberID.String())
}
