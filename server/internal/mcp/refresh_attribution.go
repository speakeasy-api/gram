package mcp

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"net/http"
)

func requestApp(ctx context.Context) mcpconnectionattempts.App {
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok && clientID != "" {
		return mcpconnectionattempts.AppOf(clientID, mcpconnectionattempts.ClientIDVerified)
	}
	return mcpconnectionattempts.AppUnverified
}

func (a *issuerGateAuthentication) refreshAttribution(ctx context.Context) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(a.endpoint, requestApp(ctx))).NotingSkips(a.skippedUpstreams).FromRequest(a.request)
}

func consentRefreshAttribution(r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) remotesessions.RefreshAttribution {
	return remotesessions.RefreshIn(connectionScope(endpoint, mcpconnectionattempts.AppOf(state.ClientID, mcpconnectionattempts.ClientIDVerified))).InAttempt(state.connectionAttempt(endpoint)).FromRequest(r)
}

func (g *metaGateContext) connectionScope(ctx context.Context) mcpconnectionattempts.Scope {
	return gatewayScope(g.organizationID, g.projectID, g.metaServerID, requestApp(ctx))
}
