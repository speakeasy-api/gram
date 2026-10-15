package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// AIM-577: a server's consent covers the sign-in configured on that server.
// A client of another authorization server belongs to another server sharing
// the organization sign-in, so this server's page neither renders it nor lets
// a posted client_id reach it.
func TestServeConsent_ServerOnlyPresentsItsOwnSignIn(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "aim577-scope")
	registerPageClient(t, ctx, fx)
	own := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim577-scope-own", "", []uuid.UUID{fx.shared})
	configureEndpointProvider(t, ctx, fx.ti, fx.endpoint, own)
	sibling := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim577-scope-sibling", "", []uuid.UUID{fx.shared, other})
	// Connected, so the page renders its cards instead of spending its
	// auto-connect on the one unlinked card.
	grant(t, ctx, fx, own, consentUpstreamA+"/")
	grant(t, ctx, fx, sibling, consentUpstreamB)

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 1 connected", "only the server's own sign-in is offered")
	require.Contains(t, html, "aim577-scope-own-rsi")
	require.NotContains(t, html, "aim577-scope-sibling-rsi")

	requireConnectActionRefused(t, fx, sibling, "unknown remote session client for this MCP server")
}

// A server that signs nobody in offers nothing, however many clients the
// organization sign-in it is gated on binds for its siblings.
func TestServeConsent_ServerWithoutSignInOffersNothing(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "aim577-none")
	registerPageClient(t, ctx, fx)
	sibling := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim577-none-sibling", "", []uuid.UUID{fx.shared, other})

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.NotContains(t, html, "aim577-none-sibling-rsi")
	requireConnectActionRefused(t, fx, sibling, "unknown remote session client for this MCP server")
}
