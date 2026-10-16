package upstreamclientsbackfill

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
	pool, err := testInfra.CloneTestDatabase(t, "upstreamclientsbackfill")
	require.NoError(t, err)
	f := &fixture{t: t, pool: pool, q: New(pool), orgID: "org_" + uuid.NewString(), projectID: uuid.New()}
	require.NoError(t, f.q.SeedOrganizationFixture(t.Context(), f.orgID))
	require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: f.projectID, Slug: "p-" + f.projectID.String()[:8], OrganizationID: f.orgID}))
	return f
}

func (f *fixture) userIssuer() uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	require.NoError(f.t, f.q.SeedUserSessionIssuerFixture(f.t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:        id,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "usi-" + id.String()[:8],
	}))
	return id
}

// client creates a project client bound to userIssuerID whose remote issuer
// identifier is issuerURL.
func (f *fixture) client(userIssuerID uuid.UUID, issuerURL string) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    issuerURL,
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(f.t, f.q.SeedClientBindingFixture(ctx, SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: userIssuerID}))
	return clientID
}

// server creates a remote-backed server on userIssuerID at upstreamURL.
func (f *fixture) server(userIssuerID uuid.UUID, upstreamURL string) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	remoteID, serverID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(ctx, SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: f.projectID, Url: upstreamURL}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(ctx, SeedMCPServerFixtureParams{
		ID:                  serverID,
		ProjectID:           f.projectID,
		Slug:                pgtype.Text{String: "s-" + serverID.String()[:8], Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remoteID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
	}))
	return serverID
}

// protectedResource records RFC 9728 metadata for resource naming
// authorizationServers.
func (f *fixture) protectedResource(resource string, authorizationServers ...string) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedProtectedResourceFixture(f.t.Context(), SeedProtectedResourceFixtureParams{
		ProjectID:            f.projectID,
		OrganizationID:       f.orgID,
		ResourceIdentifier:   resource,
		AuthorizationServers: authorizationServers,
	}))
}

func (f *fixture) associate(serverID, clientID uuid.UUID, removed bool) {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedAssociationFixture(f.t.Context(), SeedAssociationFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
		Removed:               removed,
	}))
}

func (f *fixture) associations(serverID uuid.UUID) []uuid.UUID {
	f.t.Helper()
	ids, err := f.q.ListLiveAssociationsFixture(f.t.Context(), serverID)
	require.NoError(f.t, err)
	return ids
}

func (f *fixture) run(apply bool) Report {
	f.t.Helper()
	report, err := Run(f.t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: apply, PageSize: 2})
	require.NoError(f.t, err)
	return report
}

func resultFor(t *testing.T, report Report, serverID uuid.UUID) ServerResult {
	t.Helper()
	for _, result := range report.Results {
		if result.MCPServerID == serverID {
			return result
		}
	}
	require.FailNow(t, "server not decided", serverID.String())
	return ServerResult{}
}

func TestRunDecidesOnlyWhatConfigurationProves(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// A server alone on its issuer gets every bound client, including several
	// for distinct upstreams, as hosted multi-upstream servers need.
	soleIssuer := f.userIssuer()
	sole := f.server(soleIssuer, "https://sole.example.com/mcp")
	soleA := f.client(soleIssuer, "https://as-a.example.com")
	soleB := f.client(soleIssuer, "https://as-b.example.com")

	// Two servers share an issuer binding Linear and Notion clients. Each
	// server's own metadata names its provider; the third advertises nothing.
	sharedIssuer := f.userIssuer()
	linear := f.server(sharedIssuer, "https://mcp.linear.example/mcp")
	notion := f.server(sharedIssuer, "https://mcp.notion.example/mcp")
	unknown := f.server(sharedIssuer, "https://mcp.unknown.example/mcp")
	linearClient := f.client(sharedIssuer, "https://linear.example")
	notionClient := f.client(sharedIssuer, "https://notion.example")
	f.protectedResource("https://mcp.linear.example/mcp", "https://linear.example")
	f.protectedResource("https://mcp.notion.example/mcp", "https://notion.example")

	unboundIssuer := f.userIssuer()
	unbound := f.server(unboundIssuer, "https://anon.example.com/mcp")

	preview := f.run(false)
	require.Equal(t, "dry_run", preview.Mode)
	require.Zero(t, preview.Written)
	require.Empty(t, f.associations(sole), "a dry run writes nothing")
	require.Equal(t, 1, preview.Gaps)

	applied := f.run(true)
	require.Equal(t, OutcomeSoleConsumer, resultFor(t, applied, sole).Outcome)
	require.Equal(t, OutcomeMetadataMatch, resultFor(t, applied, linear).Outcome)
	require.Equal(t, OutcomeMetadataMatch, resultFor(t, applied, notion).Outcome)
	require.Equal(t, OutcomeAmbiguous, resultFor(t, applied, unknown).Outcome)
	require.Equal(t, OutcomeUnbound, resultFor(t, applied, unbound).Outcome)
	require.Equal(t, 4, applied.Written)
	require.Equal(t, 1, applied.Gaps)

	require.ElementsMatch(t, []uuid.UUID{soleA, soleB}, f.associations(sole))
	require.Equal(t, []uuid.UUID{linearClient}, f.associations(linear))
	require.Equal(t, []uuid.UUID{notionClient}, f.associations(notion))
	require.Empty(t, f.associations(unknown), "an ambiguous server must not borrow a sibling's client")
	require.Empty(t, f.associations(unbound))

	rerun := f.run(true)
	require.Zero(t, rerun.Written, "the backfill is idempotent")
	require.Zero(t, rerun.Skipped)
	require.Equal(t, 1, rerun.Gaps)

	// Explicit configuration closes the gap.
	f.associate(unknown, linearClient, false)
	configured := f.run(true)
	require.Equal(t, OutcomeConfigured, resultFor(t, configured, unknown).Outcome)
	require.Zero(t, configured.Gaps)
}

func TestRunDoesNotUndoAnExplicitDetach(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	sharedIssuer := f.userIssuer()
	linear := f.server(sharedIssuer, "https://mcp.linear.example/mcp")
	f.server(sharedIssuer, "https://mcp.other.example/mcp")
	linearClient := f.client(sharedIssuer, "https://linear.example")
	f.protectedResource("https://mcp.linear.example/mcp", "https://linear.example")

	f.associate(linear, linearClient, true)

	report := f.run(true)
	require.Equal(t, OutcomeAmbiguous, resultFor(t, report, linear).Outcome, "a detached match is left for explicit configuration")
	require.Empty(t, f.associations(linear))
}

func TestRunSkipsAWriteWhoseEvidenceChanged(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	issuer := f.userIssuer()
	server := f.server(issuer, "https://sole.example.com/mcp")
	clientID := f.client(issuer, "https://as.example.com")

	rows, err := New(f.pool).ListUpstreamClientBackfillCandidates(t.Context(), ListUpstreamClientBackfillCandidatesParams{
		Cursor:         uuid.Nil,
		OrganizationID: f.orgID,
		PageSize:       10,
	})
	require.NoError(t, err)
	decided := Decide(rows)
	require.Equal(t, []uuid.UUID{clientID}, decided.Associate)

	// The client is unbound between the read and the write.
	require.NoError(t, f.q.DeleteClientBindingsFixture(t.Context(), clientID))

	n, err := New(f.pool).BackfillUpstreamClientAssociation(t.Context(), BackfillUpstreamClientAssociationParams{
		RemoteSessionClientID: clientID,
		McpServerID:           server,
		UserSessionIssuerID:   issuer,
		RespectRemoved:        decided.RespectRemoved,
	})
	require.NoError(t, err)
	require.Zero(t, n, "a write must yield to a concurrent configuration change")
	require.Empty(t, f.associations(server))
}

func TestDecide(t *testing.T) {
	t.Parallel()

	serverID, clientA, clientB := uuid.New(), uuid.New(), uuid.New()
	row := func(client uuid.UUID, issuerServers int64, gateway, match, live, recorded bool, liveAssociations int64) ListUpstreamClientBackfillCandidatesRow {
		return ListUpstreamClientBackfillCandidatesRow{
			McpServerID:           serverID,
			ProjectID:             uuid.Nil,
			OrganizationID:        "",
			UserSessionIssuerID:   uuid.Nil,
			RemoteBacked:          true,
			Hosted:                false,
			IssuerServerCount:     issuerServers,
			IssuerHasGateway:      gateway,
			LiveAssociations:      liveAssociations,
			RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: client != uuid.Nil},
			MetadataMatch:         match,
			PairLive:              live,
			PairRecorded:          recorded,
		}
	}

	tests := []struct {
		name      string
		rows      []ListUpstreamClientBackfillCandidatesRow
		outcome   Outcome
		associate []uuid.UUID
	}{
		{"no bound client", []ListUpstreamClientBackfillCandidatesRow{row(uuid.Nil, 1, false, false, false, false, 0)}, OutcomeUnbound, nil},
		{"sole consumer takes every client", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 1, false, false, false, false, 0), row(clientB, 1, false, false, false, false, 0)}, OutcomeSoleConsumer, []uuid.UUID{clientA, clientB}},
		{"sole consumer re-associates a removed pair", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 1, false, false, false, true, 0)}, OutcomeSoleConsumer, []uuid.UUID{clientA}},
		{"sole consumer already associated", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 1, false, false, true, true, 1)}, OutcomeSoleConsumer, nil},
		{"a gateway makes the issuer shared", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 1, true, false, false, false, 0)}, OutcomeAmbiguous, nil},
		{"one metadata match", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, true, false, false, 0), row(clientB, 2, false, false, false, false, 0)}, OutcomeMetadataMatch, []uuid.UUID{clientA}},
		{"two metadata matches are ambiguous", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, true, false, false, 0), row(clientB, 2, false, true, false, false, 0)}, OutcomeAmbiguous, nil},
		{"shared without evidence but configured", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, false, true, true, 1)}, OutcomeConfigured, nil},
		{"shared without evidence", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, false, false, false, 0)}, OutcomeAmbiguous, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Decide(tt.rows)
			require.Equal(t, tt.outcome, result.Outcome)
			require.Equal(t, tt.associate, result.Associate)
		})
	}
}
