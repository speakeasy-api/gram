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
	"github.com/speakeasy-api/gram/server/internal/feature"
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

// A consent endpoint binding clients from two authorization servers: the
// endpoint resource's advertised scopes go to the client whose grant is
// qualified to it, and the other client's request is decided by its own
// authorization server.
func TestServeConsentAction_MultiBindingConnectRequestsOwnResourceScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "scope-multi")
	fx.ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, fx.orgID, true)
	cacheResourceScopes(t, ctx, fx, consentUpstreamA+"/", []string{"a:read"})
	owner := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-multi-a", "", []uuid.UUID{fx.shared})
	shared := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-multi-b", "", []uuid.UUID{fx.shared, other})
	advertise(t, ctx, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, shared), []string{"b:read"})

	require.Equal(t, "b:read", connectScope(t, fx, shared), "provider B is decided by its own catalogue, not A's resource")
	require.Equal(t, "a:read", connectScope(t, fx, owner), "the owner requests the resource's advertised scopes")
}

// The endpoint's only client claims the endpoint's upstream even when it is
// shared with another server, so that resource's advertised scopes decide.
func TestServeConsentAction_SoleSharedClientRequestsEndpointResourceScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "scope-sole")
	fx.ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, fx.orgID, true)
	cacheResourceScopes(t, ctx, fx, consentUpstreamA+"/", []string{"a:read"})
	shared := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-sole", "", []uuid.UUID{fx.shared, other})
	advertise(t, ctx, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, shared), []string{"b:read"})

	require.Equal(t, "a:read", connectScope(t, fx, shared))
}

// The card of a client the endpoint resource does not belong to reads its
// own authorization server, as its connect does: an identity reconnect is
// offered only when that request would add openid.
func TestServeConsent_MultiBindingCardReadsOwnResourceScopes(t *testing.T) {
	t.Parallel()

	ctx, fx, other := seedSharedUpstreamEndpoint(t, "scope-card")
	fx.ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, fx.orgID, true)
	registerPageClient(t, ctx, fx)
	cacheResourceScopes(t, ctx, fx, consentUpstreamA+"/", []string{"a:read", "openid"})
	createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-card-a", "", []uuid.UUID{fx.shared})
	shared := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-card-b", "", []uuid.UUID{fx.shared, other})
	advertise(t, ctx, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, shared), []string{"b:read"})
	// A grant minted before the owner was bound routes to the endpoint.
	grantScopedTo(t, ctx, fx, shared, []string{"b:read"}, consentUpstreamA+"/")

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 2 connected")
	require.NotContains(t, html, consentIdentityCopy, "provider B's connect would not request openid")
	require.Equal(t, "b:read", connectScope(t, fx, shared))
}

// A gateway's login consults no resource row, so neither does its consent
// card: a cached row for the resource the client claims must not offer a
// reconnect that the login's issuer-override request can never satisfy.
func TestServeConsent_GatewayIdentityReconnectFollowsLogin(t *testing.T) {
	t.Parallel()

	ctx, fx, _, clientID, issuerID := metaConsent(t, "scope-gw")
	fx.ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, fx.orgID, true)
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
	fx.ti.features.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, fx.orgID, true)
	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "scope-remote", "", []uuid.UUID{fx.shared})
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
