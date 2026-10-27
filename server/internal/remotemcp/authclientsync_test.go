package remotemcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The typed auth clients these tests assert on are normally recorded by the
// organization-wide backfill. They assert that the ordinary lifecycle of a
// remote source's credential headers records and retires them on its own, with
// no backfill run in the test.

// derivedClientID is the stable id of the client derived from one
// Authorization header on a remote source, as the backfill derives it.
func derivedClientID(t *testing.T, ctx context.Context, sourceID, headerID string, clientType string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	return authclientbackfill.HeaderClientID(
		authCtx.ActiveOrganizationID,
		authclientbackfill.SourceKindRemote,
		uuid.MustParse(sourceID),
		uuid.MustParse(headerID),
		clientType,
	)
}

// authClientState reports a client's type and whether it is live. found is
// false when no row with that id exists at all.
func authClientState(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) (clientType string, live bool, found bool) {
	t.Helper()

	client, err := authclientbackfill.New(ti.conn).GetAuthClientFixture(ctx, id)
	if err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows, "read auth client %s", id)
		return "", false, false
	}

	return client.ClientType, !client.Deleted, true
}

// liveLinkedServers lists the servers offering a client through a live
// backfilled link.
func liveLinkedServers(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID) []uuid.UUID {
	t.Helper()

	servers, err := authclientbackfill.New(ti.conn).ListBackfillLinkedServersFixture(ctx, clientID)
	require.NoError(t, err)

	return servers
}

// liveHeaderCopy reads the client's live copy of one header. value is the
// stored form, so a secret compares as ciphertext and is never decrypted.
func liveHeaderCopy(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, name string) (authclientbackfill.ListAuthClientHeadersFixtureRow, bool) {
	t.Helper()

	copies, err := authclientbackfill.New(ti.conn).ListAuthClientHeadersFixture(ctx, clientID)
	require.NoError(t, err)

	for _, copied := range copies {
		if copied.Name == name {
			return copied, true
		}
	}

	return authclientbackfill.ListAuthClientHeadersFixtureRow{
		Name:                   "",
		IsSecret:               false,
		Value:                  pgtype.Text{String: "", Valid: false},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	}, false
}

// storedHeaderValue is a source header's value as the database holds it. The
// repo returns the row unrevealed, so a secret is its ciphertext.
func storedHeaderValue(t *testing.T, ctx context.Context, ti *testInstance, headerID string) pgtype.Text {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	header, err := repo.New(ti.conn).GetServerHeader(ctx, repo.GetServerHeaderParams{
		ID:        uuid.MustParse(headerID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	return header.Value
}

func requiresUpstreamAuth(t *testing.T, ctx context.Context, ti *testInstance, mcpServerID string) pgtype.Bool {
	t.Helper()

	decided, err := authclientbackfill.New(ti.conn).GetRequiresUpstreamAuthFixture(ctx, uuid.MustParse(mcpServerID))
	require.NoError(t, err)

	return decided
}

// createSourceAndServer provisions a remote source with an MCP server in front
// of it, the pairing the dashboard creates.
func createSourceAndServer(t *testing.T, ctx context.Context, ti *testInstance) *gen.CreateServerAndMcpServerResult {
	t.Helper()

	result, err := ti.service.CreateServerAndMcpServer(ctx, &gen.CreateServerAndMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                new("Remote source"),
		URL:                 "https://mcp.example.com",
		TransportType:       "streamable-http",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	return result
}

// addAuthorizationHeader sets the static secret Authorization header a legacy
// remote source carries its upstream credential in.
func addAuthorizationHeader(t *testing.T, ctx context.Context, ti *testInstance, sourceID, value string) string {
	t.Helper()

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(sourceID, "Authorization", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new(value)
		p.IsSecret = new(true)
	}))
	require.NoError(t, err)

	return header.ID
}

func TestCreateServerHeader_RecordsTypedAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer upstream-token")

	clientID := derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential")
	clientType, live, found := authClientState(t, ctx, ti, clientID)
	require.True(t, found, "the header's auth client was not recorded")
	require.Equal(t, "static_credential", clientType)
	require.True(t, live)

	require.Equal(t,
		[]uuid.UUID{uuid.MustParse(created.McpServer.ID)},
		liveLinkedServers(t, ctx, ti, clientID),
		"the server in front of the source does not offer the client",
	)

	copied, found := liveHeaderCopy(t, ctx, ti, clientID, "Authorization")
	require.True(t, found, "the header was not copied onto its auth client")
	require.True(t, copied.IsSecret)
	require.Equal(t, storedHeaderValue(t, ctx, ti, headerID), copied.Value, "the copy does not carry the header's stored value")

	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, requiresUpstreamAuth(t, ctx, ti, created.McpServer.ID))
}

// A forwarded header yields a pass_through client, under a different stable id
// than the static one, so switching how the header gets its value swaps the
// client rather than retyping it.
func TestCreateServerHeader_RecordsPassThroughAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(created.RemoteMcpServer.ID, "Authorization", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Upstream-Authorization")
	}))
	require.NoError(t, err)

	clientType, live, found := authClientState(t, ctx, ti, derivedClientID(t, ctx, created.RemoteMcpServer.ID, header.ID, "pass_through"))
	require.True(t, found)
	require.Equal(t, "pass_through", clientType)
	require.True(t, live)

	_, _, found = authClientState(t, ctx, ti, derivedClientID(t, ctx, created.RemoteMcpServer.ID, header.ID, "static_credential"))
	require.False(t, found, "a forwarded header must not yield a static credential client")
}

func TestUpdateServerHeader_UpdatesCopiedValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer first-token")

	clientID := derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential")
	before, found := liveHeaderCopy(t, ctx, ti, clientID, "Authorization")
	require.True(t, found)

	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(headerID, "Authorization", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("Bearer rotated-token")
		p.IsSecret = new(true)
	}))
	require.NoError(t, err)

	after, found := liveHeaderCopy(t, ctx, ti, clientID, "Authorization")
	require.True(t, found, "the copy was retired instead of updated")
	require.NotEqual(t, before.Value, after.Value, "the rotated value was not copied onto the auth client")
	require.Equal(t, storedHeaderValue(t, ctx, ti, headerID), after.Value)
}

func TestDeleteServerHeader_RetiresTypedAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer upstream-token")

	clientID := derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential")
	_, live, found := authClientState(t, ctx, ti, clientID)
	require.True(t, found)
	require.True(t, live)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               headerID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, live, found = authClientState(t, ctx, ti, clientID)
	require.True(t, found, "the client is retired, not erased, so its id stays taken")
	require.False(t, live, "the deleted header's auth client is still live")
	require.Empty(t, liveLinkedServers(t, ctx, ti, clientID), "a retired client is still offered by its server")

	_, found = liveHeaderCopy(t, ctx, ti, clientID, "Authorization")
	require.False(t, found, "the deleted header is still copied onto its auth client")
}

func TestDeleteServer_RetiresTypedAuthClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer upstream-token")
	clientID := derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential")

	_, live, found := authClientState(t, ctx, ti, clientID)
	require.True(t, found)
	require.True(t, live)
	require.Len(t, liveLinkedServers(t, ctx, ti, clientID), 1)

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               created.RemoteMcpServer.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, live, _ = authClientState(t, ctx, ti, clientID)
	require.False(t, live, "the deleted source's auth client is still live")
	require.Empty(t, liveLinkedServers(t, ctx, ti, clientID))
}

// A source's credential headers are usually set up before any MCP server
// fronts them, so the server records the client as it appears. Without this a
// server created after its headers would wait for the next backfill run.
func TestCreateMCPServer_AdoptsExistingSourceHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	source := createTestServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, source.ID, "Bearer upstream-token")

	clientID := derivedClientID(t, ctx, source.ID, headerID, "static_credential")
	_, _, found := authClientState(t, ctx, ti, clientID)
	require.False(t, found, "no server fronts the source yet, so no client is recorded")

	tx := testenv.BeginTx(t, ctx, ti.conn)

	mcpServer, err := mcpservers.CreateRemoteBackedMCPServer(ctx, tx, audit.NewLogger(), mcpservers.RemoteMCPMaterializationInput{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		ActorUserID:       authCtx.UserID,
		ActorEmail:        "",
		RemoteMCPServerID: uuid.MustParse(source.ID),
		DisplayName:       "Remote source",
		InitialVisibility: "disabled",
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	clientType, live, found := authClientState(t, ctx, ti, clientID)
	require.True(t, found, "the new server did not record the source's auth client")
	require.Equal(t, "static_credential", clientType)
	require.True(t, live)
	require.Equal(t, []uuid.UUID{mcpServer.ID}, liveLinkedServers(t, ctx, ti, clientID))
}

// The sync is scoped to the writer's own organization. A source id from
// another tenant is refused outright rather than read across the boundary.
func TestSyncRemoteSource_RefusesForeignOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer upstream-token")
	clientID := derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential")

	tx := testenv.BeginTx(t, ctx, ti.conn)

	err := authclientbackfill.SyncRemoteSource(ctx, tx, "org_"+uuid.NewString(), uuid.MustParse(created.RemoteMcpServer.ID))
	require.ErrorIs(t, err, authclientbackfill.ErrSourceNotFound)

	require.NoError(t, tx.Rollback(ctx))

	_, live, found := authClientState(t, ctx, ti, clientID)
	require.True(t, found)
	require.True(t, live, "a refused sync retired the owning organization's client")
	require.Len(t, liveLinkedServers(t, ctx, ti, clientID), 1)
}

// Clients and links an administrator created are not derived from the source's
// headers, so a header write leaves them alone.
func TestSyncRemoteSource_PreservesExplicitAuthClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createSourceAndServer(t, ctx, ti)
	headerID := addAuthorizationHeader(t, ctx, ti, created.RemoteMcpServer.ID, "Bearer upstream-token")

	fixtures := authclientbackfill.New(ti.conn)
	explicitID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ClientType:            "static_credential",
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		McpServerID:    uuid.MustParse(created.McpServer.ID),
		AuthClientID:   explicitID,
	}))

	// Deleting the header retires everything the source derives; the
	// administrator's records are not among them.
	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               headerID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, live, found := authClientState(t, ctx, ti, explicitID)
	require.True(t, found)
	require.True(t, live, "the sync retired an administrator's auth client")

	offered, err := fixtures.ListServerAuthClientsFixture(ctx, uuid.MustParse(created.McpServer.ID))
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{explicitID}, offered, "the server offers the administrator's client and nothing else")

	_, live, _ = authClientState(t, ctx, ti, derivedClientID(t, ctx, created.RemoteMcpServer.ID, headerID, "static_credential"))
	require.False(t, live, "the derived client was not retired with its header")
}
