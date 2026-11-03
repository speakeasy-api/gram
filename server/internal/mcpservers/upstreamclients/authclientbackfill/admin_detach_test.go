package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
)

// adminDetach detaches a client from a server the way the auth clients API
// does.
func (f *fixture) adminDetach(serverID, clientID uuid.UUID) {
	f.t.Helper()
	_, err := authclientsrepo.New(f.pool).DetachAuthClientFromServer(f.t.Context(), authclientsrepo.DetachAuthClientFromServerParams{
		AuthClientID:   clientID,
		McpServerID:    serverID,
		OrganizationID: f.orgID,
	})
	require.NoError(f.t, err)
}

// requireDetachHolds reruns the backfill and checks it neither plans nor
// writes a link for the detached server and leaves no gap.
func (f *fixture) requireDetachHolds() {
	f.t.Helper()
	dry := f.run(false)
	require.Zero(f.t, dry.PendingLinks, "a dry run must not plan to relink a detached server")
	require.Zero(f.t, dry.Gaps)
	applied := f.run(true)
	require.Zero(f.t, applied.LinksWritten, "the backfill must not relink a detached server")
	require.Zero(f.t, applied.Gaps)
}

func TestRun_AdminDetachedOAuthLinkStaysDetached(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	clientID := f.client("subject")
	first, second := f.server(), f.server()
	association := f.associate(first, clientID)
	f.associate(second, clientID)
	f.run(true)
	authClients := f.liveAuthClients()
	require.Len(t, authClients, 1)
	authClientID := authClients[0]

	f.adminDetach(first, authClientID)
	f.requireDetachHolds()

	require.Empty(t, f.links(first))
	require.Equal(t, []uuid.UUID{authClientID}, f.links(second), "the client's other servers keep it")
	require.Equal(t, []uuid.UUID{authClientID}, f.liveAuthClients(), "the shared client stays live")

	n, err := f.q.LinkAuthClientToServer(t.Context(), LinkAuthClientToServerParams{AuthClientID: authClientID, AssociationID: association, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Zero(t, n, "the write repeats the detach check")
}

func TestRun_AdminDetachedHeaderLinkStaysDetached(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	first, second := f.serverOn(source), f.serverOn(source)
	f.staticHeader(source, "Authorization")
	f.run(true)
	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	clientID := clients[0].ID

	f.adminDetach(first, clientID)
	f.requireDetachHolds()

	require.Empty(t, f.links(first))
	require.Equal(t, []uuid.UUID{clientID}, f.links(second), "the client's other servers keep it")
	require.Len(t, f.live(TypeStaticCredential), 1, "the shared client stays live")
	require.Len(t, f.headers(clientID), 1, "the shared credential stays live")

	n, err := f.q.LinkSourceServer(t.Context(), LinkSourceServerParams{
		OrganizationID:   f.orgID,
		AuthClientID:     clientID,
		McpServerID:      first,
		SourceKind:       "remote",
		SourceID:         source,
		RequiredHasOauth: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	require.Zero(t, n, "the write repeats the detach check")
}

func TestRun_AdminDetachedEMALinkStaysDetached(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi, otherUSI := f.userSessionIssuer(), f.userSessionIssuer()
	clientID, issuerID := f.emaClient()
	resource := "https://mcp.vendor.example.com/mcp"
	first := f.serverAt(resource, usi)
	second := f.serverAt(resource, otherUSI)
	f.binding(usi, issuerID, clientID, resource, []string{"read"})
	f.binding(otherUSI, issuerID, clientID, resource, []string{"read"})
	f.run(true)
	ema := f.live(TypeEMA)
	require.Len(t, ema, 1)
	emaID := ema[0].ID

	f.adminDetach(first, emaID)
	f.requireDetachHolds()

	require.NotContains(t, f.links(first), emaID)
	require.Contains(t, f.links(second), emaID, "the client's other servers keep it")
	require.Len(t, f.live(TypeEMA), 1, "the shared client stays live")

	n, err := f.q.LinkEMAServer(t.Context(), LinkEMAServerParams{OrganizationID: f.orgID, AuthClientID: emaID, McpServerID: first})
	require.NoError(t, err)
	require.Zero(t, n, "the write repeats the detach check")
}

// A dry run over an administrator-detached server whose backfilled client was
// retired counts the client it would revive but no link, as apply writes.
func TestRun_DryRunMatchesApplyForADetachedServerOfARetiredClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	clientID := f.client("subject")
	server := f.server()
	association := f.associate(server, clientID)
	f.run(true)
	authClients := f.liveAuthClients()
	require.Len(t, authClients, 1)

	f.adminDetach(server, authClients[0])
	require.NoError(t, f.q.SoftDeleteAssociationFixture(t.Context(), association))
	require.Equal(t, 1, f.run(true).ClientsRetired)
	f.associate(server, clientID)

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingClients)
	require.Zero(t, dry.PendingLinks, "a detached server gets no link")

	applied := f.run(true)
	require.Equal(t, dry.PendingClients, applied.PendingClients, "dry run matches apply")
	require.Equal(t, dry.PendingLinks, applied.PendingLinks, "dry run matches apply")
	require.Equal(t, dry.Outcomes, applied.Outcomes, "dry run matches apply")
	require.Equal(t, 1, applied.ClientsWritten)
	require.Zero(t, applied.LinksWritten)
	require.Empty(t, f.links(server))
	require.Zero(t, applied.Gaps)
}
