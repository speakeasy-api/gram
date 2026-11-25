package authclientbackfill

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSyncRemoteSource_StandaloneNormalLifecycle(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.staticHeader(source, "X-API-Key")
	id := SourceClientID(f.orgID, SourceKindRemote, source, TypeStaticCredential)
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncRemoteSource(t.Context(), tx, f.orgID, source))
	require.NoError(t, tx.Rollback(t.Context()))
	require.Empty(t, f.live(TypeStaticCredential), "rolled back writer left a catalog client")
	f.sync(source)
	require.Equal(t, []uuid.UUID{id}, f.links(server))
	require.Len(t, f.headers(id), 1)
	f.sync(source)
	require.Len(t, f.live(TypeStaticCredential), 1)
	f.adminDetach(server, id)
	require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), SetAuthClientEnabledFixtureParams{ID: id, Enabled: false}))
	f.sync(source)
	require.Empty(t, f.links(server), "writer restored an administrator detach")
	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	require.False(t, clients[0].Enabled, "writer re-enabled a disabled client")
}

func TestSyncRemoteSource_ReadyEMABlocksStandaloneBeforeCatalog(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	usi := f.userSessionIssuer()
	require.NoError(t, f.q.SetServerUserSessionIssuerFixture(t.Context(), SetServerUserSessionIssuerFixtureParams{ID: server, UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}}))
	client, issuer := f.emaClient()
	// source() uses this exact URL. No typed client is seeded or backfilled.
	f.binding(usi, issuer, client, "https://mcp.example.com/"+source.String()[:8], []string{"read"})
	f.staticHeader(source, "X-API-Key")
	f.sync(source)
	require.Empty(t, f.live(TypeStaticCredential), "EMA-owned credentials became a standalone authentication option")
	require.Empty(t, f.live(TypeEMA), "header synchronization must not create unrelated EMA clients")
	require.Empty(t, f.links(server))
}

func TestSyncEMABinding_NarrowIdempotentAndTransactional(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	server := f.serverAt(gatewayResource, usi)
	client, issuer := f.emaClient()
	binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
	otherClient, otherIssuer := f.emaClient()
	f.binding(usi, otherIssuer, otherClient, gatewayResource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Rollback(t.Context()))
	require.Empty(t, f.live(TypeEMA))
	for range 2 {
		tx := testenv.BeginTx(t, t.Context(), f.pool)
		require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
		require.NoError(t, tx.Commit(t.Context()))
	}
	id := EMAClientID(f.orgID, issuer, client, gatewayResource)
	require.Len(t, f.live(TypeEMA), 1, "normal write synchronized an unrelated binding")
	require.Equal(t, []uuid.UUID{id}, f.links(server))
	f.adminDetach(server, id)
	require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), SetAuthClientEnabledFixtureParams{ID: id, Enabled: false}))
	tx = testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	require.Empty(t, f.links(server))
	require.False(t, f.live(TypeEMA)[0].Enabled)
	tx = testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, "org_unrelated", binding))
	require.NoError(t, tx.Commit(t.Context()))
	require.Len(t, f.live(TypeEMA), 1)
}

func TestSyncEMABinding_RetiresEarlierStandaloneOption(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	usi := f.userSessionIssuer()
	require.NoError(t, f.q.SetServerUserSessionIssuerFixture(t.Context(), SetServerUserSessionIssuerFixtureParams{ID: server, UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}}))
	f.staticHeader(source, "X-API-Key")
	f.sync(source)
	require.Len(t, f.live(TypeStaticCredential), 1)
	client, issuer := f.emaClient()
	resource := "https://mcp.example.com/" + source.String()[:8]
	binding := f.binding(usi, issuer, client, resource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	id := EMAClientID(f.orgID, issuer, client, resource)
	require.Equal(t, []uuid.UUID{id}, f.links(server))
	require.Empty(t, f.live(TypeStaticCredential))
	require.Len(t, f.headers(id), 1)
}

func TestSyncRemoteSource_OAuthOwnershipBlocksStandalone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.associate(server, f.client("self"))
	f.staticHeader(source, "X-API-Key")
	f.sync(source)
	require.Empty(t, f.live(TypeStaticCredential))
	require.Empty(t, f.links(server), "header setup must not publish OAuth clients or bypass their authorization")
}
