package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

// A credential source's non-Authorization headers are copied onto the auth
// clients of the upstream OAuth client its servers offer, and the serving path
// sends those copies. Writing a server therefore has to re-decide them: a
// server that leaves a source must stop carrying the source's credentials, and
// a source that loses its last server must stop lending them.

// oauthFixture is an upstream OAuth client with the backfilled auth client the
// serving path loads header copies from.
type oauthFixture struct {
	remoteSessionClientID uuid.UUID
	authClientID          uuid.UUID
}

// seedOAuthClient records an upstream OAuth client in the auth context's
// project with the auth client the OAuth phase derives from it.
func seedOAuthClient(t *testing.T, ctx context.Context, ti *testInstance) oauthFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	fixtures := authclientbackfill.New(ti.conn)
	issuerID := uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionIssuerFixture(ctx, authclientbackfill.SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://issuer.example.com/" + issuerID.String()[:8],
		Name:      pgtype.Text{String: "", Valid: false},
	}))

	clientID := uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionClientFixture(ctx, authclientbackfill.SeedRemoteSessionClientFixtureParams{
		ID:                      clientID,
		ProjectID:               uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:          pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "client-" + clientID.String()[:8],
		CredentialOwner:         "subject",
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
	}))

	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ClientType:            "remote_session",
		Source:                "backfill",
		RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true},
	})
	require.NoError(t, err)

	return oauthFixture{remoteSessionClientID: clientID, authClientID: authClientID}
}

// associate attaches an upstream OAuth client to a server, as the identity
// flows do.
func associate(t *testing.T, ctx context.Context, ti *testInstance, serverID string, oauth oauthFixture) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := authclientbackfill.New(ti.conn).SeedAssociationFixture(ctx, authclientbackfill.SeedAssociationFixtureParams{
		ProjectID:             *authCtx.ProjectID,
		OrganizationID:        authCtx.ActiveOrganizationID,
		McpServerID:           uuid.MustParse(serverID),
		RemoteSessionClientID: oauth.remoteSessionClientID,
	})
	require.NoError(t, err)
}

// seedAuxiliaryHeader puts a secret non-Authorization credential header on a
// remote source. Only these headers are copied onto an upstream client's auth
// client; the Authorization header stays with the source's own typed client.
func seedAuxiliaryHeader(t *testing.T, ctx context.Context, ti *testInstance, sourceID uuid.UUID, name, value string) uuid.UUID {
	t.Helper()

	headerID, err := authclientbackfill.New(ti.conn).SeedRemoteHeaderFixture(ctx, authclientbackfill.SeedRemoteHeaderFixtureParams{
		RemoteMcpServerID:      sourceID,
		Name:                   name,
		IsSecret:               true,
		Value:                  pgtype.Text{String: value, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return headerID
}

// seedCopiedHeader records the copy the backfill leaves on an upstream
// client's auth client, so a test can assert a write retires it.
func seedCopiedHeader(t *testing.T, ctx context.Context, ti *testInstance, authClientID uuid.UUID, name, value string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, authclientbackfill.New(ti.conn).SeedAuthClientHeaderFixture(ctx, authclientbackfill.SeedAuthClientHeaderFixtureParams{
		AuthClientID:           authClientID,
		OrganizationID:         authCtx.ActiveOrganizationID,
		Name:                   name,
		IsRequired:             false,
		IsSecret:               true,
		Value:                  pgtype.Text{String: value, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	}))
}

// copiedHeaderNames lists the live header copies on an auth client, which are
// exactly what the serving path sends with the upstream request.
func copiedHeaderNames(t *testing.T, ctx context.Context, ti *testInstance, authClientID uuid.UUID) []string {
	t.Helper()

	copies, err := authclientbackfill.New(ti.conn).ListAuthClientHeadersFixture(ctx, authClientID)
	require.NoError(t, err)

	names := make([]string, 0, len(copies))
	for _, copied := range copies {
		names = append(names, copied.Name)
	}

	return names
}

// createServerOnRemoteSource creates an MCP server in front of a remote source
// through the real management API.
func createServerOnRemoteSource(t *testing.T, ctx context.Context, ti *testInstance, sourceID uuid.UUID) *types.McpServer {
	t.Helper()

	backendID := sourceID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "copies server " + uuid.NewString(),
		EnvironmentID:         nil,
		RemoteMcpServerID:     &backendID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	return created
}

// A server switching backend must stop carrying the headers of the source it
// left: those credentials belong to the old upstream, not the new one. The
// serving path reads these copies, so until the write retires them the old
// source's secrets keep going out with requests to the new one.
func TestUpdateMcpServer_RetiresCopiesOfTheSourceLeftBehind(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	second := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	seedAuxiliaryHeader(t, ctx, ti, first, "X-Tenant-Key", "tenant-secret")
	oauth := seedOAuthClient(t, ctx, ti)

	server := createServerOnRemoteSource(t, ctx, ti, first)
	associate(t, ctx, ti, server.ID, oauth)
	seedCopiedHeader(t, ctx, ti, oauth.authClientID, "X-Tenant-Key", "tenant-secret")
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, oauth.authClientID))

	secondID := second.String()
	_, err := ti.service.UpdateMcpServer(ctx, newBackendUpdatePayload(server.ID, "on second source", &secondID, nil))
	require.NoError(t, err)

	require.Empty(t, copiedHeaderNames(t, ctx, ti, oauth.authClientID),
		"the client still carries the credential headers of the source its server left")
}

// Deleting the last server on a source leaves the upstream client serving
// nothing from it, so it must stop carrying its credentials.
func TestDeleteMcpServer_RetiresCopiesOfItsSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	source := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	seedAuxiliaryHeader(t, ctx, ti, source, "X-Tenant-Key", "tenant-secret")
	oauth := seedOAuthClient(t, ctx, ti)

	server := createServerOnRemoteSource(t, ctx, ti, source)
	associate(t, ctx, ti, server.ID, oauth)
	seedCopiedHeader(t, ctx, ti, oauth.authClientID, "X-Tenant-Key", "tenant-secret")
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, oauth.authClientID))

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               server.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	require.Empty(t, copiedHeaderNames(t, ctx, ti, oauth.authClientID),
		"the client still carries the credentials of a source it no longer serves")
}

// Switching to a backend that carries no credential headers at all leaves no
// source to re-decide the client from, so the server itself has to be the one
// that retires the copies it took with it.
func TestUpdateMcpServer_RetiresCopiesWhenLeavingEveryCredentialSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	source := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	seedAuxiliaryHeader(t, ctx, ti, source, "X-Tenant-Key", "tenant-secret")
	oauth := seedOAuthClient(t, ctx, ti)

	server := createServerOnRemoteSource(t, ctx, ti, source)
	associate(t, ctx, ti, server.ID, oauth)
	seedCopiedHeader(t, ctx, ti, oauth.authClientID, "X-Tenant-Key", "tenant-secret")
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, oauth.authClientID))

	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()
	payload := newBackendUpdatePayload(server.ID, "now a toolset server", nil, nil)
	payload.ToolsetID = &toolsetID
	_, err := ti.service.UpdateMcpServer(ctx, payload)
	require.NoError(t, err)

	require.Empty(t, copiedHeaderNames(t, ctx, ti, oauth.authClientID),
		"the client still carries the credential headers of the source its server left")
}

// The same for a deletion: a server on no credential source is reached only
// through itself, so its clients' copies go with it.
func TestDeleteMcpServer_RetiresCopiesOfAServerOnNoSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	oauth := seedOAuthClient(t, ctx, ti)
	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "toolset server " + uuid.NewString(),
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &toolsetID,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	associate(t, ctx, ti, created.ID, oauth)
	seedCopiedHeader(t, ctx, ti, oauth.authClientID, "X-Tenant-Key", "tenant-secret")

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	require.Empty(t, copiedHeaderNames(t, ctx, ti, oauth.authClientID),
		"a deleted server's client still carries credential headers")
}

// A tunneled source gets the same typed records as a remote one, and no
// management API writes its headers, so a server arriving on it is the write
// that records them.
func TestCreateMcpServer_RecordsTunneledSourceAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	source := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	headerID, err := authclientbackfill.New(ti.conn).SeedTunneledHeaderFixture(ctx, authclientbackfill.SeedTunneledHeaderFixtureParams{
		TunneledMcpServerID:    source,
		Name:                   "Authorization",
		IsSecret:               true,
		Value:                  pgtype.Text{String: "Bearer tunneled-token", Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	backendID := source.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "tunneled server " + uuid.NewString(),
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   &backendID,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	clientID := authclientbackfill.HeaderClientID(
		authCtx.ActiveOrganizationID,
		authclientbackfill.SourceKindTunneled,
		source,
		headerID,
		"static_credential",
	)
	client, err := authclientbackfill.New(ti.conn).GetAuthClientFixture(ctx, clientID)
	require.NoError(t, err, "the tunneled source's auth client was not recorded")
	require.Equal(t, "static_credential", client.ClientType)
	require.False(t, client.Deleted)

	linked, err := authclientbackfill.New(ti.conn).ListBackfillLinkedServersFixture(ctx, clientID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.MustParse(created.ID)}, linked)
}

// newBackendUpdatePayload builds an update that replaces the backend
// references, the shape every backend switch takes.
func newBackendUpdatePayload(serverID, name string, remoteID, tunneledID *string) *gen.UpdateMcpServerPayload {
	return &gen.UpdateMcpServerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		ID:                     serverID,
		Name:                   &name,
		EnvironmentID:          nil,
		RemoteMcpServerID:      remoteID,
		TunneledMcpServerID:    tunneledID,
		ToolsetID:              nil,
		UnproxiedMcpServerID:   nil,
		ToolVariationsGroupID:  nil,
		UserSessionIssuerID:    nil,
		ClearUserSessionIssuer: nil,
		Visibility:             types.McpServerVisibility("private"),
		NetworkAccessMode:      nil,
	}
}
