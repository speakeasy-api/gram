package mcp

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"net/http"
)

func requestApp(ctx context.Context, catalog *aivendors.Snapshot) mcpconnectionattempts.App {
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok && clientID != "" {
		return mcpconnectionattempts.AppOf(catalog, clientID, mcpconnectionattempts.ClientIDAdmitted)
	}
	return mcpconnectionattempts.AppUnverified
}

func (a *issuerGateAuthentication) refreshAttribution(ctx context.Context, catalog *aivendors.Snapshot) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(a.endpoint, requestApp(ctx, catalog))).NotingSkips(a.skippedUpstreams).FromRequest(a.request)
}

func consentRefreshAttribution(catalog *aivendors.Snapshot, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(endpoint, mcpconnectionattempts.AppOf(catalog, state.ClientID, mcpconnectionattempts.ClientIDAdmitted))).InAttempt(state.connectionAttempt(catalog, endpoint)).FromRequest(r)
}

func (g *metaGateContext) connectionScope(ctx context.Context, catalog *aivendors.Snapshot) mcpconnectionattempts.Scope {
	return gatewayScope(g.organizationID, g.projectID, g.metaServerID, requestApp(ctx, catalog))
}
