// The scopes a consent page offers and a connect requests come from the
// resource the selected client's grant is qualified to, decided the same way
// on both legs; a resource another client owns, or one the login never
// resolves, never leaks into them.

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// cacheResourceScopes writes a freshly read protected resource row
// advertising scopes, so a login trusts it without probing.
func cacheResourceScopes(t *testing.T, ctx context.Context, fx consentActionFixture, resourceURL string, scopes []string) {
	t.Helper()
	_, err := remotemcp_repo.New(fx.ti.conn).UpsertRemoteProtectedResource(ctx, remotemcp_repo.UpsertRemoteProtectedResourceParams{
		ProjectID:              fx.projectID,
		OrganizationID:         fx.orgID,
		ResourceIdentifier:     resourceURL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{"https://as.example.com"},
		ScopesSupported:        scopes,
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
}

// claimResource records resourceURL as the resource the client was
// registered for, which joins the client to that resource's cached row.
func claimResource(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID, resourceURL string) {
	t.Helper()
	_, err := remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionClientResourceDisplay(ctx, remotesessions_repo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(resourceURL),
		ResourceName:          "",
		ResourceDocumentation: "",
		ResourcePolicyUri:     "",
		ResourceTosUri:        "",
		ID:                    clientID,
		ProjectID:             fx.projectID,
		OrganizationID:        fx.orgID,
	})
	require.NoError(t, err)
}

// overrideIssuerScopes pins the issuer's scope request.
func overrideIssuerScopes(t *testing.T, ctx context.Context, fx consentActionFixture, issuerID uuid.UUID, scopes []string) {
	t.Helper()
	_, err := remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionIssuer(ctx, remotesessions_repo.UpdateRemoteSessionIssuerParams{
		ScopeOverride: scopes,
		ID:            issuerID,
		ProjectID:     conv.ToNullUUID(fx.projectID),
	})
	require.NoError(t, err)
}

// connectScope is the scope parameter a connect for clientID sends upstream.
func connectScope(t *testing.T, fx consentActionFixture, clientID uuid.UUID) string {
	t.Helper()
	return postConnectAction(t, fx, clientID).Query().Get("scope")
}

// seedGatewayScopeOffers gives each provider a different offered gateway
// member. A's remote member has protected resource metadata; B's tunneled
// member falls back to the provider's scope catalogue.
func seedGatewayScopeOffers(t *testing.T, tag string, aScopes []string) (context.Context, consentActionFixture, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, fx, metaID, owner, issuerID := metaConsent(t, tag)
	a := createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, tag+"-a", consentUpstreamA+"/", conv.ToNullUUID(issuerID), 0)
	seedScopeConsentOffer(t, ctx, fx, a.mcpServerID, owner)
	cacheResourceScopes(t, ctx, fx, consentUpstreamA+"/", aScopes)

	other := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-b", "", []uuid.UUID{fx.shared})
	otherIssuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, other)
	fx = focusTypedConsentTunnel(t, ctx, fx, metaID, other, otherIssuer, tag+"-b")
	advertise(t, ctx, fx, otherIssuer, []string{"b:read"})
	return ctx, fx, owner, other
}

func seedScopeConsentOffer(t *testing.T, ctx context.Context, fx consentActionFixture, serverID, clientID uuid.UUID) {
	t.Helper()
	seedConsentAuthOffer(t, ctx, fx.ti, fx.orgID, fx.projectID, serverID, clientID, "remote_session")
	seedUserMCPConnectGrant(t, ctx, fx.ti.conn, fx.orgID, fx.subject.ID, serverID.String())
}

// A gateway with two providers scopes each connection to the member its
// typed offer names, not the other member's protected resource metadata.
func TestServeConsentAction_MultiBindingConnectRequestsOwnResourceScopes(t *testing.T) {
	t.Parallel()

	_, fx, owner, other := seedGatewayScopeOffers(t, "scope-multi", []string{"a:read"})
	fx.server = "" // Exercise provider login from the gateway, without a focused member.
	require.Equal(t, "b:read", connectScope(t, fx, other), "provider B is decided by its own catalogue, not A's resource")
	require.Equal(t, "a:read", connectScope(t, fx, owner), "the owner requests the resource's advertised scopes")
}

// The endpoint's only client claims the endpoint's upstream even when it is
// shared with another server, so that resource's advertised scopes decide.
func TestServeConsentAction_SoleSharedClientRequestsEndpointResourceScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "scope-sole")
	cacheResourceScopes(t, ctx, fx, consentUpstreamA+"/", []string{"a:read"})
	shared := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-sole", "", []uuid.UUID{fx.shared, other})
	seedConsentAuthOffer(t, ctx, fx.ti, fx.orgID, fx.projectID, fx.endpoint.McpServerID.UUID, shared, "remote_session")
	advertise(t, ctx, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, shared), []string{"b:read"})

	require.Equal(t, "a:read", connectScope(t, fx, shared))
}

// The card of a client the endpoint resource does not belong to reads its
// own authorization server, as its connect does: an identity reconnect is
// offered only when that request would add openid.
func TestServeConsent_MultiBindingCardReadsOwnResourceScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, _, other := seedGatewayScopeOffers(t, "scope-card", []string{"a:read", "openid"})
	grantScoped(t, ctx, fx, other, []string{"b:read"})
	markVerified(t, ctx, fx, other)

	// The first-party list counts connections without probing member tools.
	list := firstPartyConsent(t, ctx, fx)
	list.server = ""
	code, listing, _ := render(t, list)
	require.Equal(t, http.StatusOK, code, listing)
	require.Contains(t, listing, "1 of 2 servers connected")

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 1 connected")
	require.NotContains(t, html, consentIdentityCopy, "provider B's connect would not request openid")
	require.Equal(t, "b:read", connectScope(t, fx, other))
}

// A gateway client's login reads the claiming member's resource row, never
// the resource the client claims, so neither does its consent card: a cached
// row for the client's claimed resource must not offer a reconnect that the
// login's issuer-override request can never satisfy.
func TestServeConsent_GatewayIdentityReconnectFollowsLogin(t *testing.T) {
	t.Parallel()

	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "scope-gw")
	fx = focusTypedConsentTunnel(t, ctx, fx, metaID, clientID, issuerID, "scope-gw-member")
	const claimed = "https://claimed.example.com/mcp"
	claimResource(t, ctx, fx, clientID, claimed)
	cacheResourceScopes(t, ctx, fx, claimed, []string{"read"})
	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	overrideIssuerScopes(t, ctx, fx, issuerID, []string{"read"})
	grantScoped(t, ctx, fx, clientID, []string{"read"})

	expectIdentityHint(t, fx, false)
	require.Equal(t, "read", connectScope(t, fx, clientID))
}

// A remote endpoint's login resolves the server's own URL, never the
// resource the client claims: with no row for the server, the card falls
// through to the issuer exactly as the login does.
func TestServeConsent_RemoteEndpointIdentityReconnectFollowsLogin(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)
	ctx, fx := standaloneConsent(t, "scope-remote", upstream.URL, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, upstream.URL)
	})
	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-remote", "", []uuid.UUID{fx.shared})
	seedScopeConsentOffer(t, ctx, fx, fx.endpoint.McpServerID.UUID, clientID)
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)
	const claimed = "https://claimed.example.com/mcp"
	claimResource(t, ctx, fx, clientID, claimed)
	cacheResourceScopes(t, ctx, fx, claimed, []string{"read"})
	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	overrideIssuerScopes(t, ctx, fx, issuerID, []string{"read"})
	grantScopedTo(t, ctx, fx, clientID, []string{"read"}, upstream.URL)

	expectIdentityHint(t, fx, false)
	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, "read", loc.Query().Get("scope"))
	require.Equal(t, upstream.URL, loc.Query().Get("resource"), "the login is for the server's own resource")
}

// A gateway login reads the resource of the member it qualifies the client
// to, ahead of the issuer override, and the card offers what that login
// requests.
func TestServeConsent_GatewayMemberResourceDecidesScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "scope-gw-member")
	const member = "https://member.example.com/mcp"
	seeded := createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "scope-gw-member-m", member, conv.ToNullUUID(issuerID), 0)
	seedScopeConsentOffer(t, ctx, fx, seeded.mcpServerID, clientID)
	fx.server = "scope-gw-member-m"
	cacheResourceScopes(t, ctx, fx, member, []string{"m:read", "openid"})
	advertise(t, ctx, fx, issuerID, []string{"admin", "openid"})
	overrideIssuerScopes(t, ctx, fx, issuerID, []string{"admin"})
	grantScopedTo(t, ctx, fx, clientID, []string{"m:read"}, member)

	expectIdentityHint(t, fx, true)
	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, "m:read openid", loc.Query().Get("scope"))
	require.Equal(t, member, loc.Query().Get("resource"))
}
