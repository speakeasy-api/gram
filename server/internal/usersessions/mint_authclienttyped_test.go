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
