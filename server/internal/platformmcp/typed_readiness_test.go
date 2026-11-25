package platformmcp

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
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

// Readiness comes from the registrations
// the source's server offers through personal auth clients. A registration
// the sign-in does not bind still makes the caller's missing connection the
// finding, and several offered providers are a choice, not a misconfiguration.
func TestRemoteMCPReadinessFollowsTypedOffers(t *testing.T) {
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
			fixture := seedChainedCatalogFixtureClient(t, ctx, conn)
			fixture.serverID = typedCatalogServer(t, ctx, conn, fixture, tc.offered)

			result := fixture.probe(t, conn, nil)
			require.Equal(t, ReadinessNeedsGramAuthorization, result.State)
			require.Equal(t, "upstream_authorization_required", result.EvidenceCode)
			require.Equal(t, "no_session", result.AuthorizationIdentity.Absence)
		})
	}
}

func TestCatalogReadinessRejectsStaleRegisteredTarget(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_stale")
	require.NoError(t, err)
	f := seedChainedCatalogFixtureClient(t, ctx, conn)
	serverID := typedCatalogServer(t, ctx, conn, f, 1)
	prober := NewRemoteMCPReadinessProber(aivendorstest.NewStore(), testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), f.policy, f.sessions)
	_, err = prober.ProbeCatalogReadiness(ctx, f.principal, f.projectID, f.registrationID, serverID, f.remoteID, uuid.New(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, ErrReadinessInvalid)
}

func TestSetupConnectionFollowsTypedOffers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_setup_connection_typed")
	require.NoError(t, err)
	f := seedChainedCatalogFixtureClient(t, ctx, conn)
	serverID := typedCatalogServer(t, ctx, conn, f, 1)
	prober := NewRemoteMCPReadinessProber(aivendorstest.NewStore(), testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), f.policy, f.sessions)
	result, err := prober.TestUserConnection(ctx, f.principal.OrganizationID, f.principal.UserID, f.projectID, serverID, f.remoteID, f.userSessionIssuerID)
	require.NoError(t, err)
	require.Equal(t, "authorization_required", result.Status)
	require.Equal(t, "upstream_authorization_required", result.Code)

	_, err = prober.TestUserConnection(ctx, f.principal.OrganizationID, f.principal.UserID, f.projectID, serverID, f.remoteID, uuid.New())
	require.ErrorIs(t, err, ErrReadinessInvalid, "stale sign-in target must not use an offered credential")

	clients, _, err := prober.offeredRegistrations(ctx, f.principal.OrganizationID, f.projectID, serverID, []string{authz.AuthClientTypeRemoteSession})
	require.NoError(t, err)
	require.Len(t, clients, 1)
	_, err = remotesessionsrepo.New(conn).MarkRemoteSessionClientUpstreamRejected(ctx, remotesessionsrepo.MarkRemoteSessionClientUpstreamRejectedParams{ID: clients[0].ID, ClientID: clients[0].ExternalClientID})
	require.NoError(t, err)
	result, err = prober.TestUserConnection(ctx, f.principal.OrganizationID, f.principal.UserID, f.projectID, serverID, f.remoteID, f.userSessionIssuerID)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", result.Status)
	require.Equal(t, "user_identity_client_required", result.Code)
}

// A missing grant on the first offer must not hide a usable second offer or
// attribute its successful probe to the first provider.
func TestRemoteMCPReadinessChoosesFirstUsableTypedOffer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_readiness_usable_offer")
	require.NoError(t, err)
	f := seedChainedCatalogFixtureClient(t, ctx, conn)
	serverID := typedCatalogServer(t, ctx, conn, f, 2)
	f.serverID = serverID
	prober := NewRemoteMCPReadinessProber(aivendorstest.NewStore(), testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), f.policy, f.sessions)
	clients, incomplete, err := prober.offeredRegistrations(ctx, f.principal.OrganizationID, f.projectID, serverID, []string{authz.AuthClientTypeRemoteSession})
	require.NoError(t, err)
	require.False(t, incomplete)
	require.Len(t, clients, 2)
	chosen := clients[1]
	token, err := testenv.NewEncryptionClient(t).Encrypt([]byte("usable-offer-token"))
	require.NoError(t, err)
	session, err := remotesessionsrepo.New(conn).UpsertRemoteSessionFixture(ctx, remotesessionsrepo.UpsertRemoteSessionFixtureParams{SubjectUrn: urn.NewUserSubject(f.principal.UserID), UserSessionIssuerID: f.userSessionIssuerID, RemoteSessionClientID: chosen.ID, AccessTokenEncrypted: token, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "typed-offer-target", Version: "1.0.0"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	var allowAnonymous atomic.Bool
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization != "Bearer usable-offer-token" && (authorization != "" || !allowAnonymous.Load()) {
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

	// The same source and sign-in do not make the original offer available to
	// a sibling. The protected upstream rejects this exact target's no-auth probe.
	f.serverID = typedCatalogServer(t, ctx, conn, f, 0)
	result = f.probe(t, conn, nil)
	require.Equal(t, ReadinessNeedsConfiguration, result.State)
	require.Equal(t, "upstream_identity_provider_not_configured", result.EvidenceCode)

	// The same offer-free target can still check readiness when the upstream
	// accepts anonymous requests; it must not borrow the sibling's grant.
	allowAnonymous.Store(true)
	result = f.probe(t, conn, nil)
	require.Equal(t, ReadinessReady, result.State)
	require.Equal(t, "tools_list_ok", result.EvidenceCode)
	require.Equal(t, "anonymous", result.AuthorizationIdentity.Absence)
	require.Equal(t, uuid.Nil, result.AuthorizationIdentity.RemoteSessionClientID)
}
