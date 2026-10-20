package authclientbackfill

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var testInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	testInfra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	q         *Queries
	orgID     string
	projectID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "authclientbackfill")
	require.NoError(t, err)
	return newFixtureIn(t, pool)
}

// newFixtureIn adds another organization and project to pool.
func newFixtureIn(t *testing.T, pool *pgxpool.Pool) *fixture {
	t.Helper()
	f := &fixture{t: t, pool: pool, q: New(pool), orgID: "org_" + uuid.NewString(), projectID: uuid.New()}
	require.NoError(t, f.q.SeedOrganizationFixture(t.Context(), f.orgID))
	require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: f.projectID, Slug: "p-" + f.projectID.String()[:8], OrganizationID: f.orgID}))
	return f
}

// client creates a project client owned by owner ("subject" or "self").
func (f *fixture) client(owner string) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.example.com/" + issuerID.String()[:8],
		Name:      pgtype.Text{String: "Example", Valid: true},
	}))
	method := pgtype.Text{String: "", Valid: false}
	if owner == "self" {
		method = pgtype.Text{String: "client_secret_basic", Valid: true}
	}
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                      clientID,
		ProjectID:               uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:          pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "cid-" + clientID.String()[:8],
		CredentialOwner:         owner,
		TokenEndpointAuthMethod: method,
	}))
	return clientID
}

func (f *fixture) server() uuid.UUID {
	f.t.Helper()
	id, remoteID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(f.t.Context(), SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: f.projectID, Url: "https://mcp.example.com/" + remoteID.String()[:8]}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(f.t.Context(), SeedMCPServerFixtureParams{
		ID:                id,
		ProjectID:         f.projectID,
		Slug:              pgtype.Text{String: "s-" + id.String()[:8], Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))
	return id
}

func (f *fixture) associate(serverID, clientID uuid.UUID) uuid.UUID {
	f.t.Helper()
	id, err := f.q.SeedAssociationFixture(f.t.Context(), SeedAssociationFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
	})
	require.NoError(f.t, err)
	return id
}

func (f *fixture) run(apply bool) Report {
	f.t.Helper()
	report, err := Run(f.t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: apply, PageSize: 0})
	require.NoError(f.t, err)
	return report
}

func (f *fixture) authClients() []ListAuthClientsFixtureRow {
	f.t.Helper()
	rows, err := f.q.ListAuthClientsFixture(f.t.Context(), f.orgID)
	require.NoError(f.t, err)
	return rows
}

func (f *fixture) links(serverID uuid.UUID) []uuid.UUID {
	f.t.Helper()
	ids, err := f.q.ListServerAuthClientsFixture(f.t.Context(), serverID)
	require.NoError(f.t, err)
	return ids
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))

	report := f.run(false)

	require.Equal(t, "dry_run", report.Mode)
	require.Equal(t, 1, report.PendingClients)
	require.Equal(t, 1, report.PendingLinks)
	require.Equal(t, 1, report.Outcomes[OutcomeCreated])
	require.Empty(t, f.authClients())
	require.Empty(t, f.links(server))
}

func TestRun_ApplyIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))

	first := f.run(true)
	require.Equal(t, 1, first.ClientsWritten)
	require.Equal(t, 1, first.LinksWritten)
	clients := f.authClients()
	require.Len(t, clients, 1)
	require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))

	second := f.run(true)
	require.Zero(t, second.ClientsWritten)
	require.Zero(t, second.LinksWritten)
	require.Equal(t, 1, second.Outcomes[OutcomeExisting])
	require.Equal(t, clients, f.authClients())

	dry := f.run(false)
	require.Zero(t, dry.PendingClients)
	require.Zero(t, dry.PendingLinks)
}

func TestRun_TypesByCredentialOwner(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	subject, self := f.client("subject"), f.client("self")
	f.associate(f.server(), subject)
	f.associate(f.server(), self)

	f.run(true)

	byClient := map[uuid.UUID]ListAuthClientsFixtureRow{}
	for _, row := range f.authClients() {
		byClient[row.RemoteSessionClientID] = row
	}
	require.Equal(t, TypeRemoteSession, byClient[subject].ClientType)
	require.Equal(t, TypeServiceAccount, byClient[self].ClientType)
	require.Equal(t, "Example", byClient[subject].Name)
	require.Equal(t, uuid.NullUUID{UUID: f.projectID, Valid: true}, byClient[subject].ProjectID)
}

func TestRun_SharedClientIsOneAuthClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client("subject")
	servers := []uuid.UUID{f.server(), f.server(), f.server()}
	for _, server := range servers {
		f.associate(server, client)
	}

	report := f.run(true)

	clients := f.authClients()
	require.Len(t, clients, 1)
	require.Equal(t, 1, report.ClientsWritten)
	require.Equal(t, 3, report.LinksWritten)
	for _, server := range servers {
		require.Equal(t, []uuid.UUID{clients[0].ID}, f.links(server))
	}
}

func TestRun_StaysInItsOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	f.associate(f.server(), f.client("subject"))
	otherServer := other.server()
	other.associate(otherServer, other.client("subject"))

	f.run(true)

	require.Len(t, f.authClients(), 1)
	require.Empty(t, other.authClients())
	require.Empty(t, other.links(otherServer))
}

func TestRun_RemovedAssociationIsSkipped(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	association := f.associate(server, f.client("subject"))
	rows, err := f.q.ListAuthClientBackfillCandidates(t.Context(), ListAuthClientBackfillCandidatesParams{Cursor: uuid.Nil, OrganizationID: f.orgID, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// The association is removed after it was read: the decided writes lose.
	require.NoError(t, f.q.SoftDeleteAssociationFixture(t.Context(), association))
	report := Report{Mode: "apply", Outcomes: map[Outcome]int{}, Results: []Result{}}
	result, err := decideAndWrite(t.Context(), f.q, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0}, rows[0], map[clientKey]uuid.UUID{}, &report)
	require.NoError(t, err)

	require.Equal(t, OutcomeSkippedRace, result.Outcome)
	require.Empty(t, f.authClients())
	require.Empty(t, f.links(server))
}

func TestType(t *testing.T) {
	t.Parallel()
	for owner, want := range map[string]string{"subject": TypeRemoteSession, "self": TypeServiceAccount} {
		got, ok := Type(owner)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
	_, ok := Type("someone")
	require.False(t, ok)
}

func TestRun_GlobalClientIsOneAuthClientPerOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(t, f.q.SeedRemoteSessionIssuerFixture(t.Context(), SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.example.com/global",
		Name:      pgtype.Text{String: "", Valid: false},
	}))
	require.NoError(t, f.q.SeedRemoteSessionClientFixture(t.Context(), SeedRemoteSessionClientFixtureParams{
		ID:                      clientID,
		ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:          pgtype.Text{String: "", Valid: false},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "cid-global",
		CredentialOwner:         "subject",
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
	}))
	f.associate(f.server(), clientID)
	other.associate(other.server(), clientID)

	for _, fx := range []*fixture{f, other} {
		fx.run(true)
		clients := fx.authClients()
		require.Len(t, clients, 1)
		require.False(t, clients[0].ProjectID.Valid)
		require.Equal(t, DefaultName(TypeRemoteSession), clients[0].Name)
	}
}

func TestRun_RequiresAnOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.associate(f.server(), f.client("subject"))

	_, err := Run(t.Context(), f.pool, Options{OrganizationID: "", Apply: true, PageSize: 0})

	require.Error(t, err)
	require.Empty(t, f.authClients())
}

func TestRun_DecidesEachAssociationOnceAcrossPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client("subject")
	associations := map[uuid.UUID]bool{}
	for range 3 {
		associations[f.associate(f.server(), client)] = true
	}
	associations[f.associate(f.server(), f.client("self"))] = true

	for _, apply := range []bool{false, true} {
		report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: apply, PageSize: 1})
		require.NoError(t, err)
		seen := map[uuid.UUID]int{}
		for _, result := range report.Results {
			seen[result.AssociationID]++
		}
		require.Len(t, seen, len(associations))
		for id := range associations {
			require.Equal(t, 1, seen[id])
		}
		require.Equal(t, 2, report.PendingClients)
		require.Equal(t, 4, report.PendingLinks)
		require.Equal(t, 2, report.Outcomes[OutcomeCreated])
		require.Equal(t, 2, report.Outcomes[OutcomeAssociated])
	}
	require.Len(t, f.authClients(), 2)
}

func TestRun_UnknownOwnerIsAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("someone"))

	report := f.run(true)

	require.Equal(t, 1, report.Outcomes[OutcomeAmbiguous])
	require.Equal(t, 1, report.Gaps)
	require.Empty(t, f.authClients())
	require.Empty(t, f.links(server))
}

func TestRun_MismatchedTypeIsAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	client := f.client("subject")
	f.associate(server, client)
	_, err := f.q.SeedAuthClientFixture(t.Context(), SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:            TypeServiceAccount,
		Source:                SourceBackfill,
		RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true},
	})
	require.NoError(t, err)

	report := f.run(true)

	require.Equal(t, 1, report.Outcomes[OutcomeAmbiguous])
	require.Zero(t, report.ClientsWritten)
	require.Zero(t, report.LinksWritten)
	require.Len(t, f.authClients(), 1)
	require.Empty(t, f.links(server))
}

func (f *fixture) liveAuthClients() []uuid.UUID {
	f.t.Helper()
	ids, err := f.q.ListLiveAuthClientIDsFixture(f.t.Context(), f.orgID)
	require.NoError(f.t, err)
	return ids
}

func (f *fixture) liveLinks() []uuid.UUID {
	f.t.Helper()
	ids, err := f.q.ListLiveLinkIDsFixture(f.t.Context(), f.orgID)
	require.NoError(f.t, err)
	return ids
}

// orgClient creates an organization-level subject-owned client under a
// platform-global issuer.
func (f *fixture) orgClient() uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.example.com/" + issuerID.String()[:8],
		Name:      pgtype.Text{String: "", Valid: false},
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                      clientID,
		ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:          pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "cid-" + clientID.String()[:8],
		CredentialOwner:         "subject",
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
	}))
	return clientID
}

func TestRun_RetiresAfterAssociationHardDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	association := f.associate(server, f.client("subject"))
	f.run(true)
	require.Len(t, f.liveAuthClients(), 1)
	require.Len(t, f.liveLinks(), 1)

	require.NoError(t, f.q.HardDeleteAssociationFixture(t.Context(), association))
	dry := f.run(false)
	require.Equal(t, 1, dry.PendingRetiredClients)
	require.Equal(t, 1, dry.PendingRetiredLinks)
	require.Len(t, f.liveAuthClients(), 1, "a dry run retires nothing")

	applied := f.run(true)
	require.Equal(t, 1, applied.ClientsRetired)
	require.Equal(t, 1, applied.LinksRetired)
	require.Empty(t, f.liveAuthClients())
	require.Empty(t, f.liveLinks())

	again := f.run(true)
	require.Zero(t, again.PendingRetiredClients)
	require.Zero(t, again.PendingRetiredLinks)
}

func TestRun_RetiresAfterAssociationDetached(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client("subject")
	kept, detached := f.server(), f.server()
	f.associate(kept, client)
	association := f.associate(detached, client)
	f.run(true)
	require.Len(t, f.liveLinks(), 2)

	require.NoError(t, f.q.SoftDeleteAssociationFixture(t.Context(), association))
	report := f.run(true)

	require.Zero(t, report.ClientsRetired, "the client still serves another server")
	require.Equal(t, 1, report.LinksRetired)
	require.Len(t, f.liveAuthClients(), 1)
	require.Len(t, f.links(kept), 1)
	require.Empty(t, f.links(detached))
}

func TestRun_RetiresAfterClientDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	client := f.client("subject")
	f.associate(server, client)
	f.run(true)

	require.NoError(t, f.q.SoftDeleteRemoteSessionClientFixture(t.Context(), client))
	report := f.run(true)

	require.Equal(t, 1, report.ClientsRetired)
	require.Equal(t, 1, report.LinksRetired)
	require.Empty(t, f.liveAuthClients())
	require.Empty(t, f.liveLinks())
}

func TestRun_ClientInAnotherProjectIsAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.orgClient()
	otherProject := uuid.New()
	require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: otherProject, Slug: "p-" + otherProject.String()[:8], OrganizationID: f.orgID}))
	_, err := f.q.SeedAuthClientFixture(t.Context(), SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: otherProject, Valid: true},
		ClientType:            TypeRemoteSession,
		Source:                SourceBackfill,
		RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true},
	})
	require.NoError(t, err)
	server := f.server()
	f.associate(server, client)

	report := f.run(false)

	require.Equal(t, 1, report.Outcomes[OutcomeAmbiguous])
	require.Zero(t, report.Outcomes[OutcomeSkippedRace])
	require.Empty(t, f.links(server))
}

func TestRun_AdminClientIsNotExisting(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	client := f.client("subject")
	admin, err := f.q.SeedAuthClientFixture(t.Context(), SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:            TypeRemoteSession,
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true},
	})
	require.NoError(t, err)
	server := f.server()
	f.associate(server, client)

	report := f.run(true)

	require.Equal(t, 1, report.Outcomes[OutcomeCreated])
	require.Zero(t, report.ClientsRetired, "admin clients are never retired")
	live := f.liveAuthClients()
	require.Len(t, live, 2)
	require.Contains(t, live, admin)
	links := f.links(server)
	require.Len(t, links, 1)
	require.NotEqual(t, admin, links[0])
}

func (f *fixture) requiresUpstreamAuth(serverID uuid.UUID) pgtype.Bool {
	f.t.Helper()
	value, err := f.q.GetRequiresUpstreamAuthFixture(f.t.Context(), serverID)
	require.NoError(f.t, err)
	return value
}

func TestRun_MarksLinkedServerAsRequiringUpstreamAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))
	require.False(t, f.requiresUpstreamAuth(server).Valid)

	report := f.run(true)

	require.Equal(t, 1, report.PendingMarkedServers)
	require.Equal(t, 1, report.ServersMarked)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, f.requiresUpstreamAuth(server))
}

func TestRun_NeverOverwritesExplicitNoUpstreamAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))
	require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), SetRequiresUpstreamAuthFixtureParams{
		RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true},
		ID:                   server,
	}))

	report := f.run(true)

	require.Equal(t, 0, report.PendingMarkedServers)
	require.Equal(t, 0, report.ServersMarked)
	require.Equal(t, pgtype.Bool{Bool: false, Valid: true}, f.requiresUpstreamAuth(server))
}

func TestRun_MarkingRerunIsNoOp(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("subject"))
	f.run(true)

	rerun := f.run(true)

	require.Equal(t, 0, rerun.PendingMarkedServers)
	require.Equal(t, 0, rerun.ServersMarked)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, f.requiresUpstreamAuth(server))
}

func TestRun_DryRunCountsButDoesNotMark(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first, second := f.server(), f.server()
	client := f.client("self")
	f.associate(first, client)
	f.associate(second, client)

	dry := f.run(false)

	require.Equal(t, 2, dry.PendingMarkedServers)
	require.Equal(t, 0, dry.ServersMarked)
	require.False(t, f.requiresUpstreamAuth(first).Valid)
	require.False(t, f.requiresUpstreamAuth(second).Valid)

	applied := f.run(true)
	require.Equal(t, dry.PendingMarkedServers, applied.ServersMarked)
}

func TestRun_RetiresAfterIssuerDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	client := f.client("subject")
	f.associate(server, client)
	f.run(true)
	require.Len(t, f.liveLinks(), 1)

	require.NoError(t, f.q.SoftDeleteIssuerOfClientFixture(t.Context(), client))
	applied := f.run(true)

	require.Equal(t, 1, applied.ClientsRetired)
	require.Equal(t, 1, applied.LinksRetired)
	require.Empty(t, f.liveAuthClients())
	require.Empty(t, f.liveLinks())
}

func TestLinkAuthClientToServer_RechecksIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	client := f.client("subject")
	association := f.associate(server, client)
	f.run(true)
	authClients := f.liveAuthClients()
	require.Len(t, authClients, 1)
	require.NoError(t, f.q.SoftDeleteServerAuthClientLinksFixture(t.Context(), server))
	require.NoError(t, f.q.SoftDeleteIssuerOfClientFixture(t.Context(), client))

	n, err := f.q.LinkAuthClientToServer(t.Context(), LinkAuthClientToServerParams{
		AuthClientID:   authClients[0],
		AssociationID:  association,
		OrganizationID: f.orgID,
	})

	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, f.links(server))
}

func TestMarkServersRequiringUpstreamAuth_RequiresLiveAssociation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	association := f.associate(server, f.client("subject"))
	f.run(true)
	require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), SetRequiresUpstreamAuthFixtureParams{
		RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: false},
		ID:                   server,
	}))
	require.NoError(t, f.q.HardDeleteAssociationFixture(t.Context(), association))

	undecided, err := f.q.ListUndecidedUpstreamAuthServers(t.Context(), ListUndecidedUpstreamAuthServersParams{
		OrganizationID: f.orgID,
		McpServerIds:   []uuid.UUID{server},
	})
	require.NoError(t, err)
	require.Empty(t, undecided, "the dry-run count must match what the mark writes")

	marked, err := f.q.MarkServersRequiringUpstreamAuth(t.Context(), MarkServersRequiringUpstreamAuthParams{
		OrganizationID: f.orgID,
		McpServerIds:   []uuid.UUID{server},
	})
	require.NoError(t, err)
	require.Empty(t, marked)
	require.False(t, f.requiresUpstreamAuth(server).Valid)
}

func TestRun_AmbiguousServerIsNotMarked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server()
	f.associate(server, f.client("someone"))

	report := f.run(true)

	require.Equal(t, 1, report.Outcomes[OutcomeAmbiguous])
	require.Zero(t, report.PendingMarkedServers)
	require.Zero(t, report.ServersMarked)
	require.False(t, f.requiresUpstreamAuth(server).Valid)
}
