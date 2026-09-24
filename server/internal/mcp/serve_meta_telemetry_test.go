package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

// A gateway discovery call writes one meta_discovery row attributed to the gateway.
func TestServePublic_MetaEndpoint_DiscoveryEmitsTelemetryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

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
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      member.slug + "--alpha_tool",
		"arguments": map[string]any{},
	})

	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'alpha_tool'
		   AND meta_mcp_server_id = ?
		   AND mcp_server_id = ?`,
		1, authCtx.ProjectID.String(), meta.ID.String(), member.serverID.String())

	requireTelemetryRowCount(t, `gram_project_id = ? AND event_source = 'tool_call' AND tool_name = 'alpha_tool'`,
		1, authCtx.ProjectID.String())
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
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

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
		"arguments": map[string]any{"name": member.slug + "--alpha_tool", "arguments": map[string]any{}},
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
