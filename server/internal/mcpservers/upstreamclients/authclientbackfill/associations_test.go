package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// syncAssociations runs SyncServerAssociations for organizationID and commits,
// as the association write that added the servers' associations does.
func (f *fixture) syncAssociations(organizationID string, serverIDs ...uuid.UUID) {
	f.t.Helper()
	tx := testenv.BeginTx(f.t, f.t.Context(), f.pool)
	require.NoError(f.t, SyncServerAssociations(f.t.Context(), tx, organizationID, serverIDs...))
	require.NoError(f.t, tx.Commit(f.t.Context()))
}

// An association added after the organization's backfill run gets the typed
// client, link and header copies a run would record, so a run straight after
// finds nothing left to do and the organization stays ready.
func TestSyncServerAssociations_RecordsWhatTheBackfillWouldAfterAMigration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// A migrated organization: the backfill ran before this setup existed.
	f.run(true)

	source := f.source()
	server := f.serverOn(source)
	f.header(source, "X-Tenant-Key", true, secretValue, "")
	registration := f.client("subject")
	f.associate(server, registration)
	f.syncAssociations(f.orgID, server)

	clients := f.live(TypeRemoteSession)
	require.Len(t, clients, 1, "the new registration has no typed client")
	clientID := clients[0].ID
	require.Equal(t, OAuthClientID(f.orgID, registration, TypeRemoteSession), clientID, "the client is not under the backfill's stable id")
	require.Equal(t, uuid.NullUUID{UUID: f.projectID, Valid: true}, clients[0].ProjectID, "the client is not scoped like its registration")
	require.Equal(t, []uuid.UUID{clientID}, f.links(server))
	require.Len(t, f.headers(clientID), 1, "the source's credential header is not carried by the client")

	dry := f.run(false)
	require.Zero(t, dry.PendingClients)
	require.Zero(t, dry.PendingLinks)
	require.Zero(t, dry.PendingHeaders)
	require.Zero(t, dry.Gaps)
	require.Zero(t, dry.PendingGrants, "a personal client is covered by the existing default")

	// Idempotent: a retried write records nothing new.
	f.syncAssociations(f.orgID, server)
	require.Len(t, f.live(TypeRemoteSession), 1)
	require.Equal(t, []uuid.UUID{clientID}, f.links(server))
	require.Len(t, f.headers(clientID), 1)
}

// A self-owned registration gets a service account client, as in a run, and
// is granted nothing by the write.
func TestSyncServerAssociations_SelfOwnedRegistrationGetsAServiceAccountClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.run(true)
	grantsBefore := len(f.provisions())

	server := f.server()
	f.associate(server, f.client("self"))
	f.syncAssociations(f.orgID, server)

	clients := f.live(TypeServiceAccount)
	require.Len(t, clients, 1)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))
	require.Empty(t, f.live(TypeRemoteSession))
	require.Len(t, f.provisions(), grantsBefore, "an association write provisioned grants")
}

// Only the written servers are decided: other servers' gaps are the
// backfill's, not this write's.
func TestSyncServerAssociations_DecidesOnlyTheWrittenServers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	written, other := f.server(), f.server()
	f.associate(written, f.client("subject"))
	f.associate(other, f.client("subject"))

	f.syncAssociations(f.orgID, written)

	require.Len(t, f.links(written), 1)
	require.Empty(t, f.links(other))
	require.Len(t, f.live(TypeRemoteSession), 1)
}

// Another organization's id reaches nothing in this one, even naming its
// servers.
func TestSyncServerAssociations_StaysInItsOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	server := f.server()
	f.associate(server, f.client("subject"))

	f.syncAssociations(other.orgID, server)

	require.Empty(t, f.authClients())
	require.Empty(t, other.authClients())
	require.Empty(t, f.links(server))
	require.Error(t, SyncServerAssociations(t.Context(), testenv.BeginTx(t, t.Context(), f.pool), "", server), "no tenant scope must be refused")
}

// The records are the caller's transaction's: rolled back with it.
func TestSyncServerAssociations_RollsBackWithTheWrite(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))

	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncServerAssociations(t.Context(), tx, f.orgID, server))
	require.NoError(t, tx.Rollback(t.Context()))

	require.Empty(t, f.authClients())
	require.Empty(t, f.links(server))
}

// An administrator's detach is durable against a later association write, and
// a client an administrator disabled stays disabled when another server
// starts offering it.
func TestSyncServerAssociations_KeepsAdministratorDecisions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	registration := f.client("subject")
	detached, arriving := f.server(), f.server()
	f.associate(detached, registration)
	f.run(true)
	clients := f.live(TypeRemoteSession)
	require.Len(t, clients, 1)
	clientID := clients[0].ID

	f.adminDetach(detached, clientID)
	_, err := authclientsrepo.New(f.pool).UpdateAuthClient(t.Context(), authclientsrepo.UpdateAuthClientParams{
		Name:           clients[0].Name,
		Enabled:        false,
		ID:             clientID,
		OrganizationID: f.orgID,
	})
	require.NoError(t, err)

	f.associate(arriving, registration)
	f.syncAssociations(f.orgID, detached, arriving)

	require.Empty(t, f.links(detached), "the write relinked a server an administrator detached")
	require.Equal(t, []uuid.UUID{clientID}, f.links(arriving))
	client, err := authclientsrepo.New(f.pool).GetAuthClient(t.Context(), authclientsrepo.GetAuthClientParams{ID: clientID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.False(t, client.Enabled, "the write re-enabled a client an administrator disabled")
	require.Len(t, f.live(TypeRemoteSession), 1, "the write duplicated the backfilled client")
}

// A registration shared by servers on two sources carries neither source's
// credential headers, so the typed client the write records never sends one
// source's secret to the other's upstream.
func TestSyncServerAssociations_CopiesNoHeadersOntoAClientSharedAcrossSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.run(true)
	first, second := f.source(), f.source()
	sole, arriving := f.serverOn(first), f.serverOn(second)
	f.header(first, "X-Tenant-Key", true, secretValue, "")
	registration := f.client("subject")
	f.associate(sole, registration)
	f.associate(arriving, registration)

	f.syncAssociations(f.orgID, arriving)

	clients := f.live(TypeRemoteSession)
	require.Len(t, clients, 1)
	require.Empty(t, f.headers(clients[0].ID), "a client shared across sources carries one source's credentials")
}

func TestSyncServerAssociations_RetiresLastPrunedAssociation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	server := f.serverOn(source)
	f.header(source, "X-Tenant-Key", true, secretValue, "")
	registration := f.client("subject")
	association := f.associate(server, registration)
	f.syncAssociations(f.orgID, server)
	id := OAuthClientID(f.orgID, registration, TypeRemoteSession)
	require.Equal(t, []uuid.UUID{id}, f.links(server))
	require.Len(t, f.headers(id), 1)

	// Mirror reconciliation pruning the final legacy association, then
	// publishing its affected server in the same transaction.
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, New(tx).SoftDeleteAssociationFixture(t.Context(), association))
	require.NoError(t, SyncServerAssociations(t.Context(), tx, f.orgID, server))
	require.NoError(t, tx.Commit(t.Context()))
	require.Empty(t, f.links(server), "pruned association retained its typed link")
	require.Empty(t, f.headers(id), "pruned association retained copied credentials")
}
