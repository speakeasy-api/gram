package platformmcp

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// A missing grant on the first offer must not hide a usable second offer or
// attribute its successful probe to the first provider.
func TestRemoteMCPReadinessChoosesFirstUsableTypedOffer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_readiness_usable_offer")
	require.NoError(t, err)
	f := seedChainedCatalogFixtureClient(t, ctx, conn, false)
	_, err = productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: f.principal.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	serverID := typedCatalogServer(t, ctx, conn, f, 2)
	prober := NewRemoteMCPReadinessProber(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), f.policy, f.sessions)
	clients, err := prober.offeredRegistrations(ctx, f.principal.OrganizationID, f.projectID, []uuid.UUID{serverID}, []string{authz.AuthClientTypeRemoteSession})
	require.NoError(t, err)
	require.Len(t, clients, 2)
	chosen := clients[1]
	token, err := testenv.NewEncryptionClient(t).Encrypt([]byte("usable-offer-token"))
	require.NoError(t, err)
	session, err := remotesessionsrepo.New(conn).UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{SubjectUrn: urn.NewUserSubject(f.principal.UserID), UserSessionIssuerID: f.userSessionIssuerID, RemoteSessionClientID: chosen.ID, AccessTokenEncrypted: token, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "typed-offer-target", Version: "1.0.0"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer usable-offer-token" {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	f.policy, err = guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	_, err = remotemcprepo.New(conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{ID: f.remoteID, ProjectID: f.projectID, Url: upstream.URL, TransportType: "streamable-http"})
	require.NoError(t, err)
	result := f.probe(t, conn, nil)
	require.Equal(t, ReadinessReady, result.State)
	require.Equal(t, "tools_list_ok", result.EvidenceCode)
	require.Equal(t, chosen.RemoteSessionIssuerID, result.AuthorizationIdentity.RemoteSessionIssuerID)
	require.Equal(t, chosen.ID, result.AuthorizationIdentity.RemoteSessionClientID)
	require.Equal(t, session.ID, result.AuthorizationIdentity.RemoteSessionID)
}
