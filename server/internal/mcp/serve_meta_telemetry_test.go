package mcp_test

import (
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
