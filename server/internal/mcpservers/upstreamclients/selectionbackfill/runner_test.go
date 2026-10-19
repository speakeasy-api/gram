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
	n, err := usersessionsrepo.New(f.pool).RecordUpstreamClientSelections(f.t.Context(), usersessionsrepo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: userIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        credentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
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
		f.server(issuer)
		f.server(uuid.Nil)
		credential := f.session(issuer)

		f.record(issuer, credential)

		require.Equal(t, map[uuid.UUID]uuid.UUID{
			linear: linearClient,
			notion: notionClient,
		}, f.selections(credential),
			"a server with several eligible clients, or none associated while its issuer binds some, is left as a gap; a server off the issuer is not reached")
	})

	t.Run("records no-auth only where nothing is configured", func(t *testing.T) {
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

	t.Run("a gateway member its gateway's clients make ambiguous is never no-auth", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		gatewayIssuer := f.projectIssuer()
		// The member's own issuer binds nothing, but two of the gateway's
		// clients hold grants for its upstream, so neither is decided and the
		// member is not a server that needs no authentication.
		member := f.server(f.projectIssuer())
		first, second := f.client(gatewayIssuer), f.client(gatewayIssuer)
		for _, client := range []uuid.UUID{first, second} {
			require.NoError(t, f.q.SeedGrantFixture(t.Context(), SeedGrantFixtureParams{
				SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()[:8]),
				UserSessionIssuerID:   gatewayIssuer,
				RemoteSessionClientID: client,
				Resource:              pgtype.Text{String: "https://upstream.example.com/mcp", Valid: true},
			}))
		}
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

		require.Zero(t, f.record(gatewayIssuer, credential))
		require.Empty(t, f.selections(credential))

		report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
		require.NoError(t, err)
		require.Equal(t, []Gap{{CredentialID: credential, MCPServerID: member, Outcome: OutcomeUnassociated}}, report.Gaps)
	})

	t.Run("another project's gateway client does not keep a member from no-auth", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		gatewayIssuer := f.projectIssuer()
		member := f.server(f.projectIssuer())
		// A client owned by another project is never one this member uses,
		// even bound to the gateway's issuer with a grant naming its upstream.
		other := uuid.New()
		require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: other, Slug: "p-" + other.String()[:8], OrganizationID: f.orgID}))
		foreignIssuer, foreign := uuid.New(), uuid.New()
		require.NoError(t, f.q.SeedRemoteSessionIssuerFixture(t.Context(), SeedRemoteSessionIssuerFixtureParams{
			ID:        foreignIssuer,
			ProjectID: uuid.NullUUID{UUID: other, Valid: true},
			Slug:      "rsi-" + foreignIssuer.String()[:8],
			Issuer:    "https://" + foreignIssuer.String()[:8] + ".example.com",
		}))
		require.NoError(t, f.q.SeedRemoteSessionClientFixture(t.Context(), SeedRemoteSessionClientFixtureParams{
			ID:                    foreign,
			ProjectID:             uuid.NullUUID{UUID: other, Valid: true},
			OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
			RemoteSessionIssuerID: foreignIssuer,
			ClientID:              "cid-" + foreign.String()[:8],
		}))
		require.NoError(t, f.q.SeedClientBindingFixture(t.Context(), SeedClientBindingFixtureParams{RemoteSessionClientID: foreign, UserSessionIssuerID: gatewayIssuer}))
		require.NoError(t, f.q.SeedGrantFixture(t.Context(), SeedGrantFixtureParams{
			SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()[:8]),
			UserSessionIssuerID:   gatewayIssuer,
			RemoteSessionClientID: foreign,
			Resource:              pgtype.Text{String: "https://upstream.example.com/mcp", Valid: true},
		}))
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

		require.Equal(t, map[uuid.UUID]uuid.UUID{member: uuid.Nil}, f.selections(credential))
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
		{"already chosen", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: true, EligibleClients: 2, AssociatedClients: 2, IssuerBoundClients: 2}, OutcomeSelected},
		{"one eligible client", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 1, AssociatedClients: 2, IssuerBoundClients: 3}, OutcomeClient},
		{"several eligible clients", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 2, AssociatedClients: 2, IssuerBoundClients: 2}, OutcomeAmbiguous},
		{"several eligible clients, one connected", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 2, ConnectedClients: 1, AssociatedClients: 2, IssuerBoundClients: 2}, OutcomeClient},
		{"several eligible clients, several connected", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 2, ConnectedClients: 2, AssociatedClients: 2, IssuerBoundClients: 2}, OutcomeAmbiguous},
		{"associated but not through this issuer", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 1, IssuerBoundClients: 1}, OutcomeUnreachableClient},
		{"unassociated with bound clients", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 0, IssuerBoundClients: 1}, OutcomeUnassociated},
		{"unassociated with a gateway client tied to it", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 0, IssuerBoundClients: 0, EvidenceClients: 2}, OutcomeUnassociated},
		{"nothing configured", ListUpstreamClientSelectionCoverageRow{McpServerID: uuid.Nil, Selected: false, EligibleClients: 0, AssociatedClients: 0, IssuerBoundClients: 0}, OutcomeNoAuth},
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

// A grant is one subject's credential for one client, whichever issuer
// minted it, so grants minted through a sibling issuer for the gateway's
// clients still keep a member from being recorded as needing no auth.
func TestRecordUpstreamClientSelectionsCountsGrantsMintedThroughAnotherIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gatewayIssuer := f.projectIssuer()
	siblingIssuer := f.projectIssuer()
	member := f.server(f.projectIssuer())
	for _, client := range []uuid.UUID{f.client(gatewayIssuer, siblingIssuer), f.client(gatewayIssuer, siblingIssuer)} {
		require.NoError(t, f.q.SeedGrantFixture(t.Context(), SeedGrantFixtureParams{
			SubjectUrn:            urn.NewUserSubject("user-" + uuid.NewString()[:8]),
			UserSessionIssuerID:   siblingIssuer,
			RemoteSessionClientID: client,
			Resource:              pgtype.Text{String: "https://upstream.example.com/mcp", Valid: true},
		}))
	}
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

	require.Zero(t, f.record(gatewayIssuer, credential))
	require.Empty(t, f.selections(credential))

	report, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, []Gap{{CredentialID: credential, MCPServerID: member, Outcome: OutcomeUnassociated}}, report.Gaps)
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

// connect records subject's live grant for clientID, as connecting its card
// at consent does.
func (f *fixture) connect(userIssuerID, clientID uuid.UUID, subject urn.SessionSubject) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedGrantFixture(f.t.Context(), SeedGrantFixtureParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		Resource:              pgtype.Text{String: "https://upstream.example.com/mcp", Valid: true},
	}))
}

// When a server offers several clients, the card the person connected at
// consent is the credential's choice; connecting none or several leaves it to
// a reconnection.
func TestRecordUpstreamClientSelectionsChoosesTheConnectedClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	first, second := f.client(issuer), f.client(issuer)
	server := f.server(issuer, first, second)

	connectedOne := urn.NewUserSubject("connected-one")
	f.connect(issuer, second, connectedOne)
	one := f.sessionFor(issuer, connectedOne)

	connectedBoth := urn.NewUserSubject("connected-both")
	f.connect(issuer, first, connectedBoth)
	f.connect(issuer, second, connectedBoth)
	both := f.sessionFor(issuer, connectedBoth)

	none := f.session(issuer)

	preview, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 1, preview.Pairs[OutcomeClient], "the prediction agrees with what is recorded")
	require.Equal(t, 2, preview.Pairs[OutcomeAmbiguous])

	f.record(issuer, one)
	f.record(issuer, both)
	f.record(issuer, none)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: second}, f.selections(one))
	require.Empty(t, f.selections(both))
	require.Empty(t, f.selections(none))
}

// A platform client is shared by every organization, so grants for it minted
// in another organization never keep this organization's member from no-auth.
func TestRecordUpstreamClientSelectionsIgnoresGrantsMintedInAnotherOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	gatewayIssuer := f.projectIssuer()
	member := f.server(f.projectIssuer())
	remoteIssuerID, clientID := uuid.New(), uuid.New()
	require.NoError(t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        remoteIssuerID,
		ProjectID: uuid.NullUUID{},
		Slug:      "rsi-" + remoteIssuerID.String()[:8],
		Issuer:    "https://" + remoteIssuerID.String()[:8] + ".example.com",
	}))
	require.NoError(t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: gatewayIssuer}))
	otherOrg, otherIssuer := "org_"+uuid.NewString(), uuid.New()
	require.NoError(t, f.q.SeedOrganizationFixture(ctx, otherOrg))
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, SeedUserSessionIssuerFixtureParams{
		ID:             otherIssuer,
		ProjectID:      uuid.NullUUID{},
		OrganizationID: pgtype.Text{String: otherOrg, Valid: true},
		Slug:           "usi-" + otherIssuer.String()[:8],
	}))
	f.connect(otherIssuer, clientID, urn.NewUserSubject("elsewhere"))
	gatewayID := uuid.New()
	require.NoError(t, f.q.SeedMetaMCPServerFixture(ctx, SeedMetaMCPServerFixtureParams{
		ID:                  gatewayID,
		OrganizationID:      f.orgID,
		ProjectID:           f.projectID,
		Name:                "gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: gatewayIssuer, Valid: true},
	}))
	require.NoError(t, f.q.SeedMetaMCPServerMemberFixture(ctx, SeedMetaMCPServerMemberFixtureParams{ProjectID: f.projectID, MetaMcpServerID: gatewayID, McpServerID: member}))
	credential := f.session(gatewayIssuer)

	f.record(gatewayIssuer, credential)
	require.Equal(t, map[uuid.UUID]uuid.UUID{member: uuid.Nil}, f.selections(credential))
}
