package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// sync runs SyncRemoteSource for one source and commits, as the write that
// changed the source's headers or servers does.
func (f *fixture) sync(sourceID uuid.UUID) {
	f.t.Helper()
	tx := testenv.BeginTx(f.t, f.t.Context(), f.pool)
	require.NoError(f.t, SyncRemoteSource(f.t.Context(), tx, f.orgID, sourceID))
	require.NoError(f.t, tx.Commit(f.t.Context()))
}

// A source without an Authorization header keeps the client the backfill
// recorded, but only the servers still backed by the source offer it.
// Otherwise a server that switched backend would go on sending the old
// source's credential headers.
func TestSyncRemoteSource_RetiresLinkOfAServerMovedOffASource(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source, other := f.source(), f.source()
	moved, stays := f.serverOn(source), f.serverOn(source)
	f.staticHeader(source, "X-API-Key")
	f.run(true)

	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	clientID := clients[0].ID
	require.Equal(t, []uuid.UUID{clientID}, f.links(moved))
	require.Equal(t, []uuid.UUID{clientID}, f.links(stays))

	require.NoError(t, f.q.MoveServerSourceFixture(t.Context(), MoveServerSourceFixtureParams{
		ID:                moved,
		RemoteMcpServerID: uuid.NullUUID{UUID: other, Valid: true},
	}))
	f.sync(source)

	require.Empty(t, f.links(moved), "a server off the source still offers its credential client")
	require.Equal(t, []uuid.UUID{clientID}, f.links(stays), "the source's own servers keep it")
	require.Len(t, f.live(TypeStaticCredential), 1, "the client itself is the backfill's to retire")
}

// The source's last server leaving retires the client with its links: that
// needs no reading of upstream OAuth or EMA clients, so the sync decides it as
// the backfill does.
func TestSyncRemoteSource_RetiresAClientWhoseLastServerLeft(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source, other := f.source(), f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "X-API-Key")
	f.run(true)
	require.Len(t, f.live(TypeStaticCredential), 1)

	require.NoError(t, f.q.MoveServerSourceFixture(t.Context(), MoveServerSourceFixtureParams{
		ID:                server,
		RemoteMcpServerID: uuid.NullUUID{UUID: other, Valid: true},
	}))
	f.sync(source)

	require.Empty(t, f.live(TypeStaticCredential), "the client outlived the last server on its source")
	require.Empty(t, f.links(server))
}

// A sync records what a backfill run would, so a run straight after one finds
// nothing left to do and reports no gap.
func TestSyncRemoteSource_LeavesTheBackfillNothingToDo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	headerID := f.staticHeader(source, "Authorization")
	f.sync(source)

	clientID := HeaderClientID(f.orgID, SourceKindRemote, source, headerID, TypeStaticCredential)
	require.Equal(t, []uuid.UUID{clientID}, f.links(server))
	require.Len(t, f.headers(clientID), 1)

	dry := f.run(false)
	require.Zero(t, dry.PendingClients, "the sync left a client for the backfill to record")
	require.Zero(t, dry.PendingLinks, "the sync left a link for the backfill to record")
	require.Zero(t, dry.PendingHeaders, "the sync left a header copy for the backfill to write")
	require.Zero(t, dry.PendingRetiredClients)
	require.Zero(t, dry.PendingRetiredLinks)
	require.Zero(t, dry.Gaps)
}
