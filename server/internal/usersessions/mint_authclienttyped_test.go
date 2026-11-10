package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// This invokes the mint service and verifies the persisted choice for its real
// credential. It is not an HTTP or enforced MCP request test.
func TestMintUserSessionRecordsTypedClientAfterNormalCapture(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, auth)
	require.NotNil(t, auth.ProjectID)
	orgID := auth.ActiveOrganizationID
	_, err := authclientbackfill.Run(ctx, ti.conn, authclientbackfill.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	issuerID := seedIssuer(t, ctx, ti, "mint-typed-issuer")
	source, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     *auth.ProjectID,
		Name:          conv.ToPGText("Mint upstream"),
		Slug:          conv.ToPGText("mint-upstream"),
		TransportType: "streamable-http",
		Url:           "https://mint.example.test/mcp",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           *auth.ProjectID,
		Name:                conv.ToPGText("Mint server"),
		Slug:                conv.ToPGText("mint-server"),
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
		RemoteMcpServerID:   conv.ToNullUUID(source.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	seedMintEndpoint(t, ctx, ti, server.ID, uuid.NullUUID{})
	registration := seedUpstream(t, ctx, ti.conn, uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, server.UserSessionIssuerID.UUID, urn.NewUserSubject(auth.UserID), "mint-typed.example.test")
	require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, orgID, *auth.ProjectID, []uuid.UUID{server.UserSessionIssuerID.UUID}))
	serverID := server.ID.String()
	minted, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{McpServerID: &serverID})
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(minted.AccessToken, urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String())
	require.NoError(t, err)
	session, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: server.UserSessionIssuerID.UUID, Jti: claims.ID})
	require.NoError(t, err)
	choice, err := authclientbackfill.New(ti.conn).GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: session.ID, McpServerID: server.ID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: authclientbackfill.OAuthClientID(orgID, registration, authclientbackfill.TypeRemoteSession), Valid: true}, choice.AuthClientID)
}

// With upstream auth enforced, a dashboard mint records each server's choice
// from the auth clients it offers. An upstream OAuth association whose auth
// client the server no longer offers is not a choice.
func TestMintUserSessionRecordsChoicesFromAuthClientsUnderEnforcement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, auth)
	require.NotNil(t, auth.ProjectID)
	orgID := auth.ActiveOrganizationID
	issuerID := seedIssuer(t, ctx, ti, "mint-enforced-issuer")
	servers := make([]mcpserversrepo.McpServer, 0, 2)
	for _, slug := range []string{"mint-offered", "mint-detached"} {
		source, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
			ID:            uuid.New(),
			ProjectID:     *auth.ProjectID,
			Name:          conv.ToPGText(slug),
			Slug:          conv.ToPGText(slug + "-upstream"),
			TransportType: "streamable-http",
			Url:           "https://" + slug + ".example.test/mcp",
		})
		require.NoError(t, err)
		server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID:                  uuid.New(),
			ProjectID:           *auth.ProjectID,
			Name:                conv.ToPGText(slug),
			Slug:                conv.ToPGText(slug),
			UserSessionIssuerID: conv.ToNullUUID(issuerID),
			RemoteMcpServerID:   conv.ToNullUUID(source.ID),
			Visibility:          "private",
		})
		require.NoError(t, err)
		servers = append(servers, server)
	}
	offered, detached := servers[0], servers[1]
	seedMintEndpoint(t, ctx, ti, offered.ID, uuid.NullUUID{})

	fixtures := authclientbackfill.New(ti.conn)
	// The offered server's auth client has no association.
	registration := seedUpstream(t, ctx, ti.conn, uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, issuerID, urn.NewUserSubject(auth.UserID), "mint-offered.example.test")
	authClient, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: authclientbackfill.TypeRemoteSession, Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: orgID, McpServerID: offered.ID, AuthClientID: authClient, Source: "admin"}))
	// The other server keeps an association the issuer binds after its offer was detached.
	bound := seedUpstream(t, ctx, ti.conn, uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, issuerID, urn.NewUserSubject(auth.UserID), "mint-bound.example.test")
	_, err = fixtures.SeedAssociationFixture(ctx, authclientbackfill.SeedAssociationFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: orgID, McpServerID: detached.ID, RemoteSessionClientID: bound})
	require.NoError(t, err)
	inherited, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, ClientType: authclientbackfill.TypeRemoteSession, Source: "backfill", RemoteSessionClientID: uuid.NullUUID{UUID: bound, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: orgID, McpServerID: detached.ID, AuthClientID: inherited, Source: "backfill"}))
	require.NoError(t, fixtures.SoftDeleteServerAuthClientLinksFixture(ctx, detached.ID))
	require.NoError(t, fixtures.SeedOrganizationFeatureFixture(ctx, authclientbackfill.SeedOrganizationFeatureFixtureParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)}))

	serverID := offered.ID.String()
	minted, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{McpServerID: &serverID})
	require.NoError(t, err)
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(minted.AccessToken, urn.NewUserSessionIssuer(issuerID).String())
	require.NoError(t, err)
	session, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: claims.ID})
	require.NoError(t, err)

	choice, err := fixtures.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: session.ID, McpServerID: offered.ID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: authClient, Valid: true}, choice.AuthClientID)
	require.Equal(t, uuid.NullUUID{UUID: registration, Valid: true}, choice.RemoteSessionClientID)

	choice, err = fixtures.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: session.ID, McpServerID: detached.ID})
	require.NoError(t, err)
	require.True(t, choice.NoAuth)
	require.False(t, choice.RemoteSessionClientID.Valid)
	require.False(t, choice.AuthClientID.Valid)
}
