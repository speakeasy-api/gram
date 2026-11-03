package tunneledmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A tunneled source carries credential headers the same way a remote one
// does, and the backfill derives the same typed auth clients, links and header
// copies from them. No management API writes those headers, so the source's
// own lifecycle is what has to retire the records: nothing else says the
// headers are gone.

// Deleting a tunneled source retires the auth client derived from its
// Authorization header, with the link that offered it and its header copy.
func TestDeleteServerRetiresDerivedAuthClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	source := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	fixtures := authclientbackfill.New(ti.conn)
	headerID, err := fixtures.SeedTunneledHeaderFixture(ctx, authclientbackfill.SeedTunneledHeaderFixtureParams{
		TunneledMcpServerID:    source.ID,
		Name:                   "Authorization",
		IsSecret:               true,
		Value:                  pgtype.Text{String: "Bearer tunneled-token", Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	serverID := uuid.New()
	require.NoError(t, fixtures.SeedTunneledMCPServerFixture(ctx, authclientbackfill.SeedTunneledMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Slug:                pgtype.Text{String: "ts-" + serverID.String()[:8], Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: source.ID, Valid: true},
	}))

	// The records the backfill leaves for this source, which the sync has to
	// retire when the source goes.
	report, err := authclientbackfill.Run(ctx, ti.conn, authclientbackfill.Options{
		OrganizationID: authCtx.ActiveOrganizationID,
		Apply:          true,
		PageSize:       0,
	})
	require.NoError(t, err)
	require.Zero(t, report.Gaps)

	clientID := authclientbackfill.HeaderClientID(
		authCtx.ActiveOrganizationID,
		authclientbackfill.SourceKindTunneled,
		source.ID,
		headerID,
		authclientbackfill.TypeStaticCredential,
	)
	client, err := fixtures.GetAuthClientFixture(ctx, clientID)
	require.NoError(t, err, "the backfill recorded no client for the tunneled source")
	require.False(t, client.Deleted)

	linked, err := fixtures.ListBackfillLinkedServersFixture(ctx, clientID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{serverID}, linked)

	copies, err := fixtures.ListAuthClientHeadersFixture(ctx, clientID)
	require.NoError(t, err)
	require.Len(t, copies, 1)

	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	require.NoError(t, ti.service.DeleteServer(writeCtx, &gen.DeleteServerPayload{
		ID:               source.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	client, err = fixtures.GetAuthClientFixture(ctx, clientID)
	require.NoError(t, err, "the client is retired, not erased, so its id stays taken")
	require.True(t, client.Deleted, "the deleted tunneled source's auth client is still live")

	linked, err = fixtures.ListBackfillLinkedServersFixture(ctx, clientID)
	require.NoError(t, err)
	require.Empty(t, linked, "a retired client is still offered by its server")

	copies, err = fixtures.ListAuthClientHeadersFixture(ctx, clientID)
	require.NoError(t, err)
	require.Empty(t, copies, "the deleted source's header is still copied onto its client")
}

// A tunneled source the organization does not own is refused outright, so one
// project cannot reach another's records through this path.
func TestSyncTunneledSourceRefusesForeignOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	source := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	tx := testenv.BeginTx(t, ctx, ti.conn)

	err := authclientbackfill.SyncTunneledSource(ctx, tx, "org_"+uuid.NewString(), source.ID)
	require.ErrorIs(t, err, authclientbackfill.ErrSourceNotFound)

	// The owning organization still reaches it.
	require.NoError(t, authclientbackfill.SyncTunneledSource(ctx, tx, authCtx.ActiveOrganizationID, source.ID))
}
