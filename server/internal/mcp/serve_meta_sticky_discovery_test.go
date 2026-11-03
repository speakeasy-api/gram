package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

// staleToolMessage is the actionable part of the refusal a client gets when it
// calls a tool from a tool list its connection no longer serves.
const staleToolMessage = "reconnect to refresh the tool list"

func listGatewayTools(t *testing.T, ti *testInstance, slug, token string) string {
	t.Helper()
	response, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return response.Body.String()
}

// TestGatewayDefaultConsentKeepsModeAfterDefaultChanges pins that a connection
// approved with "Gateway default" keeps the mode it was approved with: MCP
// clients cache tools/list and the gateway advertises no listChanged, so a
// default change must reach existing connections only when they reconnect.
func TestGatewayDefaultConsentKeepsModeAfterDefaultChanges(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "sticky-mode-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, newCatalogUpstream(t, 1).url)

	existing := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, "", nil)
	require.Contains(t, listGatewayTools(t, ti, slug, existing.token), `"name":"execute_tool"`)

	setGatewayMode(t, ctx, ti, gateway.ID, string(metamcp.DiscoveryModeDirect))

	listed := listGatewayTools(t, ti, slug, existing.token)
	require.Contains(t, listed, `"name":"execute_tool"`, "an existing connection keeps its mode")
	require.NotContains(t, listed, "source--tool_000")
	require.NotNil(t, existing.policy.Gateway.DiscoveryMode, "consent records the default it was approved with")
	require.Equal(t, metamcp.DiscoveryModeProgressive, *existing.policy.Gateway.DiscoveryMode)
	require.Contains(t, callGatewayTool(t, ti, slug, existing.token, metamcp.ToolListServers, map[string]any{}), `\"slug\":\"source\"`)

	refreshed := postRefreshGrant(t, ti, slug, client.ClientID, existing.refresh, "")
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &tokens))
	require.Contains(t, listGatewayTools(t, ti, slug, tokens.AccessToken), `"name":"execute_tool"`, "refresh keeps the approved mode")

	fresh := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, "", nil)
	require.NotNil(t, fresh.policy.Gateway.DiscoveryMode)
	require.Equal(t, metamcp.DiscoveryModeDirect, *fresh.policy.Gateway.DiscoveryMode, "a new connection gets the current default")
	listed = listGatewayTools(t, ti, slug, fresh.token)
	require.Contains(t, listed, "source--tool_000")
	require.NotContains(t, listed, `"name":"execute_tool"`)
}

// TestGatewayDefaultConsentCodeModeFallsBackWhileUnavailable pins that a
// connection approved with a Code Mode default stores Code Mode, and is still
// served Progressive while the code runtime is unavailable.
func TestGatewayDefaultConsentCodeModeFallsBackWhileUnavailable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "sticky-code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, newCatalogUpstream(t, 1).url)
	enableCodeMode(t, ctx, ti)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
	setGatewayMode(t, ctx, ti, gateway.ID, string(metamcp.DiscoveryModeCode))

	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, "", nil)
	require.Contains(t, listGatewayTools(t, ti, slug, connection.token), `"name":"execute"`)

	setGatewayMode(t, ctx, ti, gateway.ID, string(metamcp.DiscoveryModeProgressive))
	require.Contains(t, listGatewayTools(t, ti, slug, connection.token), `"name":"execute"`, "an existing connection keeps Code Mode")
	require.NotNil(t, connection.policy.Gateway.DiscoveryMode)
	require.Equal(t, metamcp.DiscoveryModeCode, *connection.policy.Gateway.DiscoveryMode)
	stale := callGatewayTool(t, ti, slug, connection.token, metamcp.ToolListServers, map[string]any{})
	require.Contains(t, stale, `tool \"list_servers\" is not available on this connection`)
	require.Contains(t, stale, staleToolMessage)

	mcp.SetTestCodeExecutor(ti.service.Service, codemode.Disabled{})
	listed := listGatewayTools(t, ti, slug, connection.token)
	require.Contains(t, listed, `"name":"execute_tool"`, "Code Mode falls back to Progressive while the runtime is unavailable")
	require.NotContains(t, listed, `"name":"execute"`)
}

// TestGatewayStaleToolCallAsksClientToReconnect pins the refusal a client gets
// when it calls a tool its connection's mode does not serve, as it does after
// caching a tool list from a different mode.
func TestGatewayStaleToolCallAsksClientToReconnect(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "stale-tool-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, newCatalogUpstream(t, 1).url)
	progressive := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive, nil)
	direct := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)

	for _, tc := range []struct {
		name  string
		token string
		tool  string
	}{
		{name: "progressive/code mode tool", token: progressive.token, tool: metamcp.ToolCodeExecute},
		{name: "progressive/direct tool", token: progressive.token, tool: "source--tool_000"},
		{name: "direct/progressive tool", token: direct.token, tool: metamcp.ToolExecuteTool},
		{name: "direct/code mode tool", token: direct.token, tool: metamcp.ToolCodeExecute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := callGatewayTool(t, ti, slug, tc.token, tc.tool, map[string]any{})
			require.Contains(t, body, `"code":-32002`)
			require.Contains(t, body, `tool \"`+tc.tool+`\" is not available on this connection`)
			require.Contains(t, body, staleToolMessage)
		})
	}
}
