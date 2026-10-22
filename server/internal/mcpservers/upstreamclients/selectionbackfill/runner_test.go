package selectionbackfill

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	pool, err := testInfra.CloneTestDatabase(t, "selectionbackfill")
	require.NoError(t, err)
	f := &fixture{t: t, pool: pool, q: New(pool), orgID: "org_" + uuid.NewString(), projectID: uuid.New()}
	require.NoError(t, f.q.SeedOrganizationFixture(t.Context(), f.orgID))
	require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: f.projectID, Slug: "p-" + f.projectID.String()[:8], OrganizationID: f.orgID}))
	return f
}

func (f *fixture) projectIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedUserSessionIssuerFixture(f.t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:             id,
		ProjectID:      uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID: pgtype.Text{String: f.orgID, Valid: true},
		Slug:           "usi-" + id.String()[:8],
	}))
	return id
}

func (f *fixture) organizationIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedUserSessionIssuerFixture(f.t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:             id,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID: pgtype.Text{String: f.orgID, Valid: true},
		Slug:           "usi-" + id.String()[:8],
	}))
	return id
}

// client creates a project client bound to each of userIssuerIDs.
func (f *fixture) client(userIssuerIDs ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://" + issuerID.String()[:8] + ".example.com",
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	for _, userIssuerID := range userIssuerIDs {
		require.NoError(f.t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userIssuerID}))
	}
	return clientID
}

// server creates a server on userIssuerID (uuid.Nil for none) associated with
// clientIDs.
func (f *fixture) server(userIssuerID uuid.UUID, clientIDs ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	backendID, serverID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(ctx, SeedRemoteMCPServerFixtureParams{ID: backendID, ProjectID: f.projectID}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(ctx, SeedMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           f.projectID,
		Slug:                pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: backendID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: userIssuerID != uuid.Nil},
	}))
	for _, clientID := range clientIDs {
		f.associate(serverID, clientID)
	}
	return serverID
}

func (f *fixture) associate(serverID, clientID uuid.UUID) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedAssociationFixture(f.t.Context(), SeedAssociationFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
	}))
}

// session creates a live session for a new person on userIssuerID and
// returns its credential.
func (f *fixture) session(userIssuerID uuid.UUID) uuid.UUID {
	f.t.Helper()
	return f.sessionFor(userIssuerID, urn.NewUserSubject("user-"+uuid.NewString()[:8]))
}

// sessionFor creates a live session for subject on userIssuerID and returns
// its credential.
func (f *fixture) sessionFor(userIssuerID uuid.UUID, subject urn.SessionSubject) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedUserSessionFixture(f.t.Context(), SeedUserSessionFixtureParams{
		ID:                  id,
		ProjectID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:      pgtype.Text{String: f.orgID, Valid: true},
		UserSessionIssuerID: userIssuerID,
		SubjectUrn:          subject,
		Jti:                 id.String(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}))
	return id
}

func (f *fixture) record(userIssuerID, credentialID uuid.UUID) int64 {
	f.t.Helper()
	return f.recordPicked(userIssuerID, credentialID, nil)
}

// recordPicked records choices as issuance does after a consent page that
// picked a client for each server in picks.
func (f *fixture) recordPicked(userIssuerID, credentialID uuid.UUID, picks map[uuid.UUID]uuid.UUID) int64 {
	f.t.Helper()
	servers, clients := []uuid.UUID{}, []uuid.UUID{}
	for serverID, clientID := range picks {
		servers = append(servers, serverID)
		clients = append(clients, clientID)
	}
	n, err := usersessionsrepo.New(f.pool).RecordUpstreamClientSelections(f.t.Context(), usersessionsrepo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: userIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        credentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
		PickedMcpServerIds:  servers,
		PickedClientIds:     clients,
	})
	require.NoError(f.t, err)
	return n
}

// selections maps each chosen server to its client, uuid.Nil for no-auth.
func (f *fixture) selections(credentialID uuid.UUID) map[uuid.UUID]uuid.UUID {
	f.t.Helper()
	rows, err := f.q.ListSelectionsFixture(f.t.Context(), ListSelectionsFixtureParams{
		CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialID:   credentialID,
	})
	require.NoError(f.t, err)
	chosen := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, row := range rows {
		require.Equal(f.t, row.NoAuth, !row.RemoteSessionClientID.Valid)
		chosen[row.McpServerID] = row.RemoteSessionClientID.UUID
	}
	return chosen
}

func (f *fixture) selection(userIssuerID, credentialID, serverID uuid.UUID) usersessionsrepo.GetUpstreamClientSelectionRow {
	f.t.Helper()
	row, err := usersessionsrepo.New(f.pool).GetUpstreamClientSelection(f.t.Context(), usersessionsrepo.GetUpstreamClientSelectionParams{
		UserSessionIssuerID: userIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        credentialID,
		McpServerID:         serverID,
		OrganizationID:      f.orgID,
	})
	require.NoError(f.t, err)
	return row
}

func TestRecordUpstreamClientSelections(t *testing.T) {
	t.Parallel()

	t.Run("chooses only what configuration decides", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		issuer := f.projectIssuer()
		linearClient := f.client(issuer)
		notionClient := f.client(issuer)
		otherClient := f.client(issuer)

		linear := f.server(issuer, linearClient)
		notion := f.server(issuer, notionClient)
		f.server(issuer, linearClient, otherClient)
		unassociated := f.server(issuer)
		f.server(uuid.Nil)
		credential := f.session(issuer)

		f.record(issuer, credential)

		require.Equal(t, map[uuid.UUID]uuid.UUID{
			linear:       linearClient,
			notion:       notionClient,
			unassociated: uuid.Nil,
		}, f.selections(credential),
			"a server with several eligible clients is left as a gap, one with no associated client uses none whatever its issuer binds, and a server off the issuer is not reached")
	})

	t.Run("records no-auth where no client is associated", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		issuer := f.organizationIssuer()
		bareIssuer := f.projectIssuer()
		anonymous := f.server(bareIssuer)
		credential := f.session(issuer)

		f.record(issuer, credential)

		require.Equal(t, map[uuid.UUID]uuid.UUID{anonymous: uuid.Nil}, f.selections(credential),
			"the organization issuer reaches every server in the organization")
		got := f.selection(issuer, credential, anonymous)
		require.True(t, got.NoAuth)
		require.True(t, got.Usable)

		// No row is a different answer from no-auth.
		missing := f.server(bareIssuer, f.client(bareIssuer))
		_, err := usersessionsrepo.New(f.pool).GetUpstreamClientSelection(t.Context(), usersessionsrepo.GetUpstreamClientSelectionParams{
			UserSessionIssuerID: issuer,
			CredentialKind:      upstreamclients.CredentialKindUserSession,
			CredentialID:        credential,
			McpServerID:         missing,
			OrganizationID:      f.orgID,
		})
		require.Error(t, err)
	})

	t.Run("a gateway member is chosen through the gateway's issuer", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		memberIssuer := f.projectIssuer()
		gatewayIssuer := f.projectIssuer()
		memberClient := f.client(memberIssuer, gatewayIssuer)
		member := f.server(memberIssuer, memberClient)
		gatewayID := uuid.New()
		require.NoError(t, f.q.SeedMetaMCPServerFixture(t.Context(), SeedMetaMCPServerFixtureParams{
			ID:                  gatewayID,
			OrganizationID:      f.orgID,
			ProjectID:           f.projectID,
			Name:                "gateway",
			UserSessionIssuerID: uuid.NullUUID{UUID: gatewayIssuer, Valid: true},
		}))
		require.NoError(t, f.q.SeedMetaMCPServerMemberFixture(t.Context(), SeedMetaMCPServerMemberFixtureParams{ProjectID: f.projectID, MetaMcpServerID: gatewayID, McpServerID: member}))
		credential := f.session(gatewayIssuer)

		f.record(gatewayIssuer, credential)

		require.Equal(t, map[uuid.UUID]uuid.UUID{member: memberClient}, f.selections(credential))
	})

	t.Run("a choice is kept, never replaced", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		issuer := f.projectIssuer()
		first := f.client(issuer)
		server := f.server(issuer, first)
		person := urn.NewUserSubject("same-person")
		earlier := f.sessionFor(issuer, person)
		f.record(issuer, earlier)

		// The administrator moves the server to another client.
		second := f.client(issuer)
		require.NoError(t, f.q.RemoveAssociationFixture(t.Context(), RemoveAssociationFixtureParams{McpServerID: server, RemoteSessionClientID: first}))
		f.associate(server, second)
		later := f.sessionFor(issuer, person)
		f.record(issuer, later)
		require.Zero(t, f.record(issuer, earlier), "recording again is idempotent")

		require.Equal(t, first, f.selections(earlier)[server], "an earlier credential keeps its own choice")
		require.Equal(t, second, f.selections(later)[server], "credentials of the same principal hold distinct choices")

		stale := f.selection(issuer, earlier, server)
		require.False(t, stale.Usable, "a detached client makes the choice unusable rather than switching clients")
		require.True(t, f.selection(issuer, later, server).Usable)

		// Unbinding the client from the credential's issuer makes it unusable
		// too, though it stays associated with the server.
		require.NoError(t, f.q.UnbindClientFixture(t.Context(), UnbindClientFixtureParams{RemoteSessionClientID: second, UserSessionIssuerID: issuer}))
		require.False(t, f.selection(issuer, later, server).Usable)
	})

	t.Run("another organization's servers are not reached", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		other := newFixture(t)
		other.server(uuid.Nil)

		issuer := f.organizationIssuer()
		mine := f.server(uuid.Nil)
		credential := f.session(issuer)
		f.record(issuer, credential)

		require.Equal(t, map[uuid.UUID]uuid.UUID{mine: uuid.Nil}, f.selections(credential))
	})
}

func TestRunBackfillsLiveCredentials(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	client := f.client(issuer)
	server := f.server(issuer, client)
	gapServer := f.server(issuer, client, f.client(issuer))
	credential := f.session(issuer)

	preview, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 1, preview.Credentials)
	require.Equal(t, 1, preview.Pairs[OutcomeClient])
	require.Equal(t, 1, preview.Pairs[OutcomeAmbiguous])
	require.Equal(t, []Gap{{CredentialID: credential, MCPServerID: gapServer, Outcome: OutcomeAmbiguous}}, preview.Gaps)
	require.Empty(t, f.selections(credential), "a dry run writes nothing")

	applied, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), applied.Written)
	require.Zero(t, applied.FullyCovered, "the ambiguous server still blocks this credential")
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: client}, f.selections(credential))

	rerun, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 1})
	require.NoError(t, err)
	require.Zero(t, rerun.Written, "the backfill is idempotent")
	require.Equal(t, 1, rerun.Pairs[OutcomeSelected])
	require.Len(t, rerun.Gaps, 1)
}

func TestClassifyMatchesTheRecordedChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		row  ListUpstreamClientSelectionCoverageRow
		want Outcome
	}{
		{"already chosen", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: true, ChoseClient: true, EligibleClients: 2, AssociatedClients: 2}, OutcomeSelected},
		{"chosen client no longer usable", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, ChoseClient: true, EligibleClients: 1, AssociatedClients: 1}, OutcomeUnusableClient},
		{"no-auth choice the server's new client makes unusable", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, ChoseClient: false, EligibleClients: 1, AssociatedClients: 1}, OutcomeClient},
		{"one eligible client", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 1, AssociatedClients: 2}, OutcomeClient},
		{"several eligible clients", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 2, AssociatedClients: 2}, OutcomeAmbiguous},
		{"associated but not through this issuer", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 1}, OutcomeUnreachableClient},
		{"no associated client", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 0}, OutcomeNoAuth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Classify(tt.row))
		})
	}
}

func TestRecordUpstreamClientSelectionsSkipsUnproxiedServers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	f.client(issuer)
	backendID, serverID := uuid.New(), uuid.New()
	require.NoError(t, f.q.SeedUnproxiedMCPServerFixture(t.Context(), SeedUnproxiedMCPServerFixtureParams{ID: backendID, ProjectID: f.projectID}))
	require.NoError(t, f.q.SeedUnproxiedMCPServerRowFixture(t.Context(), SeedUnproxiedMCPServerRowFixtureParams{
		ID:                   serverID,
		ProjectID:            f.projectID,
		Slug:                 pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		UnproxiedMcpServerID: uuid.NullUUID{UUID: backendID, Valid: true},
		UserSessionIssuerID:  uuid.NullUUID{UUID: issuer, Valid: true},
	}))
	credential := f.session(issuer)

	require.Zero(t, f.record(issuer, credential), "an unproxied server keeps the issuer's bindings and needs no choice")
	report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Empty(t, report.Gaps, "and is never a gap")
}

// beforeSelectionWrite runs a change just before the backfill records a
// credential's choices, standing in for a configuration change that races
// the run.
type beforeSelectionWrite struct {
	DBTX
	change func()
}

func (db beforeSelectionWrite) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "INSERT INTO upstream_client_selections") {
		db.change()
	}
	return db.DBTX.Exec(ctx, sql, args...) //nolint:wrapcheck,glint // notestingrawsql: forwards the generated statement unchanged; the test double only observes it.
}

func TestRunReportsCoverageFromWhatWasRecorded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	server := f.server(issuer, f.client(issuer))
	credential := f.session(issuer)

	// A second eligible client is associated between the coverage read and
	// the write, so the write correctly records nothing.
	racing := beforeSelectionWrite{DBTX: f.pool, change: func() { f.associate(server, f.client(issuer)) }}
	report, err := Run(t.Context(), racing, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 1, report.Pairs[OutcomeClient], "the prediction made before the write")
	require.Zero(t, report.Written)
	require.Zero(t, report.FullyCovered, "an unrecorded pair is not covered")
	require.Equal(t, []Gap{{CredentialID: credential, MCPServerID: server, Outcome: OutcomeAmbiguous}}, report.Gaps)
	require.Empty(t, f.selections(credential))
}

// When a server offers several clients, the one picked on the consent page is
// the credential's choice; without a pick, as for a credential the backfill
// covers, it is left to a reconnection. A pick that is not one of the
// server's eligible clients chooses nothing.
func TestRecordUpstreamClientSelectionsRecordsTheConsentPick(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	first, second := f.client(issuer), f.client(issuer)
	server := f.server(issuer, first, second)
	stranger := f.client(f.projectIssuer())

	picked := f.session(issuer)
	unpicked := f.session(issuer)
	foreign := f.session(issuer)

	preview, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 3, preview.Pairs[OutcomeAmbiguous], "the backfill has no consent page to pick from")

	f.recordPicked(issuer, picked, map[uuid.UUID]uuid.UUID{server: second})
	f.record(issuer, unpicked)
	f.recordPicked(issuer, foreign, map[uuid.UUID]uuid.UUID{server: stranger})
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: second}, f.selections(picked))
	require.Empty(t, f.selections(unpicked))
	require.Empty(t, f.selections(foreign))
}

func TestChoiceNamingTheServersHeadersReadsAsNoAuth(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	server := f.server(issuer)
	credential := f.session(issuer)
	f.record(issuer, credential)
	headers, err := f.q.SeedHeaderAuthClientFixture(t.Context(), SeedHeaderAuthClientFixtureParams{OrganizationID: f.orgID, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, f.q.NameAuthClientOnSelectionFixture(t.Context(), NameAuthClientOnSelectionFixtureParams{AuthClientID: headers, CredentialID: credential, McpServerID: server}))

	covered, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 1, covered.Pairs[OutcomeSelected], "the server's headers serve it while it has no associated client")

	client := f.client(issuer)
	f.associate(server, client)
	require.Equal(t, int64(1), f.record(issuer, credential), "a client decided later replaces it, as it replaces a no-auth choice")
	row := f.selection(issuer, credential, server)
	require.Equal(t, uuid.NullUUID{UUID: client, Valid: true}, row.RemoteSessionClientID)
	require.True(t, row.Usable)
	require.False(t, row.NoAuth)
}
