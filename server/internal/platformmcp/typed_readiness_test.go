package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// typedCatalogServer fronts the fixture's remote source with an MCP server
// that offers count personal OAuth registrations, each of its own provider.
// No registration is bound to the fixture's sign-in or associated with the
// server, so only the typed offer reaches it.
func typedCatalogServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, f chainedCatalogFixture, count int) uuid.UUID {
	t.Helper()
	slug := "typed-readiness-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(f.userSessionIssuerID), RemoteMcpServerID: conv.ToNullUUID(f.remoteID), Visibility: "private",
	})
	require.NoError(t, err)
	fixtures := authclientbackfill.New(conn)
	for range count {
		issuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
			ProjectID: conv.ToNullUUID(f.projectID), OrganizationID: conv.ToPGText(f.principal.OrganizationID),
			Slug: "typed-readiness-" + uuid.NewString()[:8], Issuer: "https://" + uuid.NewString()[:8] + ".provider.example.test",
			ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
			TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		})
		require.NoError(t, err)
		registration, err := remotesessionsrepo.New(conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
			ProjectID: conv.ToNullUUID(f.projectID), OrganizationID: conv.ToPGText(f.principal.OrganizationID), RemoteSessionIssuerID: issuer.ID,
			ClientID: "typed-readiness-client", ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{},
		})
		require.NoError(t, err)
		authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: f.principal.OrganizationID, ProjectID: conv.ToNullUUID(f.projectID), ClientType: authz.AuthClientTypeRemoteSession, Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration.ID)})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: f.projectID, OrganizationID: f.principal.OrganizationID, McpServerID: server.ID, AuthClientID: authClientID, Source: "admin"}))
	}
	return server.ID
}

// With upstream auth enforcement on, readiness comes from the registrations
// the source's server offers through personal auth clients. A registration
// the sign-in does not bind still makes the caller's missing connection the
// finding, and several offered providers are a choice, not a misconfiguration.
func TestRemoteMCPReadinessFollowsTypedOffersUnderEnforcement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		offered int
	}{
		{name: "one offered registration the sign-in does not bind", offered: 1},
		{name: "several offered providers", offered: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_typed")
			require.NoError(t, err)
			fixture := seedChainedCatalogFixtureClient(t, ctx, conn, false)
			_, err = productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fixture.principal.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
			require.NoError(t, err)
			typedCatalogServer(t, ctx, conn, fixture, tc.offered)

			result := fixture.probe(t, conn, nil)
			require.Equal(t, ReadinessNeedsGramAuthorization, result.State)
			require.Equal(t, "upstream_authorization_required", result.EvidenceCode)
			require.Equal(t, "no_session", result.AuthorizationIdentity.Absence)
		})
	}
}

// The setup connection test asks for the user's own grant on the server's
// User Identity client. With upstream auth enforcement on that client is the
// registration the server offers through a personal auth client, so an
// unconnected user is told to sign in; while the sign-in link decides, the
// same server has no client to test.
func TestSetupConnectionFollowsTypedOffersUnderEnforcement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		enforced bool
		status   string
		code     string
	}{
		{name: "typed auth clients enforced", enforced: true, status: "authorization_required", code: "upstream_authorization_required"},
		{name: "sign-in link decides", enforced: false, status: "configuration_required", code: "user_identity_client_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_setup_connection_typed")
			require.NoError(t, err)
			fixture := seedChainedCatalogFixtureClient(t, ctx, conn, false)
			if tc.enforced {
				_, err = productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fixture.principal.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
			}
			serverID := typedCatalogServer(t, ctx, conn, fixture, 1)

			prober := NewRemoteMCPReadinessProber(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), fixture.policy, fixture.sessions)
			result, err := prober.TestUserConnection(ctx, fixture.principal.OrganizationID, fixture.principal.UserID, fixture.projectID, serverID, fixture.remoteID, fixture.userSessionIssuerID)
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			require.Equal(t, tc.code, result.Code)
		})
	}
}
