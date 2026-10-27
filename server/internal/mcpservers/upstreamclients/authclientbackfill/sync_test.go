package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))

	require.NoError(t, f.q.MoveServerSourceFixture(t.Context(), MoveServerSourceFixtureParams{
		ID:                server,
		RemoteMcpServerID: uuid.NullUUID{UUID: other, Valid: true},
	}))
	f.sync(source)

	require.Empty(t, f.live(TypeStaticCredential), "the client outlived the last server on its source")
	require.Empty(t, f.links(server))
}

// A sync records what a backfill run would, so a run straight after one finds
// nothing left to do and reports no gap. The source carries an Authorization
// header for its own typed client and another credential header copied onto
// the upstream OAuth client its server offers, so the equivalence covers the
// clients, the links and the copies.
func TestSyncRemoteSource_LeavesTheBackfillNothingToDo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	oauthClientID := f.client("subject")
	f.associate(server, oauthClientID)
	// The state a deployed organization is in: the backfill has run, and the
	// headers below are the writes that arrive after it.
	f.run(true)
	oauthAuthClients := f.liveAuthClients()
	require.Len(t, oauthAuthClients, 1, "the backfill recorded no client for the association")

	headerID := f.staticHeader(source, "Authorization")
	f.header(source, "X-Tenant-Key", true, secretValue, "")
	f.sync(source)

	clientID := HeaderClientID(f.orgID, SourceKindRemote, source, headerID, TypeStaticCredential)
	require.Contains(t, f.links(server), clientID)
	require.Len(t, f.headers(clientID), 2, "the source's credential headers are carried by its own client")
	require.Len(t, f.headers(oauthAuthClients[0]), 1, "only the non-Authorization header reaches the upstream client")

	dry := f.run(false)
	require.Zero(t, dry.PendingClients, "the sync left a client for the backfill to record")
	require.Zero(t, dry.PendingLinks, "the sync left a link for the backfill to record")
	require.Zero(t, dry.PendingHeaders, "the sync left a header copy for the backfill to write")
	require.Zero(t, dry.PendingRetiredClients)
	require.Zero(t, dry.PendingRetiredLinks)
	require.Zero(t, dry.Gaps)
}

// A source's credential headers other than Authorization are copied onto the
// auth clients of the upstream OAuth and EMA clients its servers offer, and the
// serving path sends those copies. A sync has to keep them in line as the
// source is written, with the same ownership rule the backfill applies.

// syncTunneled runs SyncTunneledSource for one source and commits.
func (f *fixture) syncTunneled(sourceID uuid.UUID) {
	f.t.Helper()
	tx := testenv.BeginTx(f.t, f.t.Context(), f.pool)
	require.NoError(f.t, SyncTunneledSource(f.t.Context(), tx, f.orgID, sourceID))
	require.NoError(f.t, tx.Commit(f.t.Context()))
}

// A source write re-decides the copies on the ema client its server offers, so
// a header added after the backfill run reaches that client too.
func TestSyncRemoteSource_CopiesAuxiliaryHeaderOntoEMAClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	source, server := f.emaSourceAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.run(true)

	ema := f.live(TypeEMA)
	require.Len(t, ema, 1, "the backfill recorded no ema client")
	emaID := ema[0].ID
	require.Contains(t, f.links(server), emaID)
	require.Empty(t, f.headers(emaID), "the source carries no credential header yet")

	// The header the source gains after the run.
	headerID := f.header(source, "X-Tenant-Key", true, secretValue, "")
	f.sync(source)

	copies := f.headers(emaID)
	require.Len(t, copies, 1, "the new credential header was not copied onto the ema client")
	require.Equal(t, "X-Tenant-Key", copies[0].Name)

	// Losing the header retires the copy the serving path would still send.
	require.NoError(t, f.q.SoftDeleteRemoteHeaderFixture(t.Context(), headerID))
	f.sync(source)
	require.Empty(t, f.headers(emaID), "the deleted header is still copied onto the ema client")
}

// An ema client offered by servers on two sources with credential headers
// carries neither one's, so a header write on either source retires the copies.
func TestSyncRemoteSource_RefusesCopyOntoEMAClientSharedAcrossSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	first, _ := f.emaSourceAt(resource, usi)
	second, _ := f.emaSourceAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.header(first, "X-Tenant-Key", true, secretValue, "")
	f.run(true)

	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	emaID := ema[0].ID
	require.Empty(t, f.headers(emaID), "a client shared across sources carries one source's credentials")

	f.header(second, "X-Other-Key", true, secretValue, "")
	f.sync(second)
	require.Empty(t, f.headers(emaID), "a header write made a shared client carry one source's credentials")
}

// A tunneled source's deletion retires the copies its headers left on the
// upstream clients its servers offer, not only its own typed client.
func TestSyncTunneledSource_RetiresCopiesWhenTheSourceGoes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	oauthClientID := f.client("subject")
	source := uuid.New()
	require.NoError(t, f.q.SeedTunneledSourceFixture(t.Context(), SeedTunneledSourceFixtureParams{
		ID:                 source,
		ProjectID:          f.projectID,
		Name:               "tunnel-" + source.String()[:8],
		KeyHash:            "kh-" + source.String(),
		ResourceIdentifier: pgtype.Text{String: "", Valid: false},
	}))
	serverID := uuid.New()
	require.NoError(t, f.q.SeedTunneledMCPServerFixture(t.Context(), SeedTunneledMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           f.projectID,
		Slug:                pgtype.Text{String: "ts-" + serverID.String()[:8], Valid: true},
		TunneledMcpServerID: uuid.NullUUID{UUID: source, Valid: true},
	}))
	_, err := f.q.SeedTunneledHeaderFixture(t.Context(), SeedTunneledHeaderFixtureParams{
		TunneledMcpServerID:    source,
		Name:                   "X-Tenant-Key",
		IsSecret:               true,
		Value:                  pgtype.Text{String: secretValue, Valid: true},
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	f.associate(serverID, oauthClientID)
	f.run(true)

	authClients := f.liveAuthClients()
	require.Len(t, authClients, 1, "the backfill recorded no client for the association")
	require.Len(t, f.headers(authClients[0]), 1, "the tunneled source's header was not copied")

	require.NoError(t, f.q.SoftDeleteTunneledSourceFixture(t.Context(), source))
	f.syncTunneled(source)

	require.Empty(t, f.headers(authClients[0]),
		"the deleted tunneled source's header is still sent by the upstream client")
}

// An ema client whose last live server is gone serves nothing, so the copies
// the serving path would still send with its requests are retired. Its link
// survives the server tombstone, so the live server list, not the link, is
// what says the client is spent.
func TestSyncRemoteSource_RetiresEMACopiesWhenTheLastServerIsDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	source, server := f.emaSourceAt(resource, usi)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.header(source, "X-Tenant-Key", true, secretValue, "")
	f.run(true)

	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	emaID := ema[0].ID
	require.Len(t, f.headers(emaID), 1, "the source's header was not copied onto the ema client")

	require.NoError(t, f.q.SoftDeleteMCPServerFixture(t.Context(), server))
	f.sync(source)

	require.Empty(t, f.headers(emaID),
		"a client with no live server still carries the source's credentials")
}
