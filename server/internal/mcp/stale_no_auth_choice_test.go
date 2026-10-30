package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// A member consented to with no sign-in later gains a sign-in client. The
// stored no-auth choice no longer serves, and the call says what fixes it:
// authorizing the gateway again, which a provider sign-in alone does not do.
func TestStaleNoAuthChoiceSaysHowToFixIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	slug := "stale-no-auth-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)

	ti.service.SetAuthClientPermission(func() bool { return true })
	ti.service.manual = true
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeLive, metamcp.DiscoveryModeDirect, nil)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(connection.token, urn.NewUserSessionIssuer(issuer.ID).String())
	require.NoError(t, err)
	session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
	require.NoError(t, err)
	selection := func() upstreamclientsrepo.GetAuthClientSelectionRow {
		rows, err := upstreamclientsrepo.New(ti.conn).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{OrganizationID: auth.ActiveOrganizationID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: session.ID, McpServerIds: []uuid.UUID{serverID}})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}
	require.True(t, selection().NoAuth, "consent recorded no sign-in for the member")
	require.Contains(t, callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{}), "executed live")

	// An administrator adds a personal sign-in client to the member.
	registration := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "late-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	fixtures := authclientfixtures.New(ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))

	// Calls stop being served without sign-in, connected or not.
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, registration, urn.NewUserSubject(mockidp.MockUserID), "fixture-token", upstream.url)
	before := upstream.calls.Load()
	result := callGatewayTool(t, ti, slug, connection.token, "source--tool_000", map[string]any{})
	require.Contains(t, result, "This server's sign-in changed since you connected. Sign in to this gateway again from your AI client to choose its connection.")
	require.Equal(t, before, upstream.calls.Load())
	require.True(t, selection().NoAuth, "the stored choice is left for a new consent to replace")

	// Authorizing again records the connected client, and calls are served.
	renewed := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeLive, metamcp.DiscoveryModeDirect, nil)
	require.Contains(t, callGatewayTool(t, ti, slug, renewed.token, "source--tool_000", map[string]any{}), "executed live")
}
