package associationbackfill

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
	pool, err := testInfra.CloneTestDatabase(t, "associationbackfill")
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
	// server's own metadata names its provider; the third has no evidence for
	// either, so it uses no client.
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
	require.Zero(t, preview.Gaps)

	applied := f.run(true)
	require.Equal(t, OutcomeSoleConsumer, resultFor(t, applied, sole).Outcome)
	require.Equal(t, OutcomeMetadataMatch, resultFor(t, applied, linear).Outcome)
	require.Equal(t, OutcomeMetadataMatch, resultFor(t, applied, notion).Outcome)
	require.Equal(t, OutcomeNoClient, resultFor(t, applied, unknown).Outcome)
	require.Equal(t, OutcomeUnbound, resultFor(t, applied, unbound).Outcome)
	require.Equal(t, 4, applied.Written)
	require.Zero(t, applied.Gaps)

	require.ElementsMatch(t, []uuid.UUID{soleA, soleB}, f.associations(sole))
	require.Equal(t, []uuid.UUID{linearClient}, f.associations(linear))
	require.Equal(t, []uuid.UUID{notionClient}, f.associations(notion))
	require.Empty(t, f.associations(unknown), "a server without evidence must not borrow a sibling's client")
	require.Empty(t, f.associations(unbound))

	rerun := f.run(true)
	require.Zero(t, rerun.Written, "the backfill is idempotent")
	require.Zero(t, rerun.Skipped)
	require.Zero(t, rerun.Gaps)

	// Explicit configuration gives it a client.
	f.associate(unknown, linearClient, false)
	configured := f.run(true)
	require.Equal(t, OutcomeConfigured, resultFor(t, configured, unknown).Outcome)
	require.Zero(t, configured.Gaps)
}

func TestRunDecidesASharedIssuerServerFromGrantsThenDerivedIssuer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Three servers share an issuer and publish no protected resource
	// metadata, so only grants and derived issuers can decide them.
	sharedIssuer := f.userIssuer()
	granted := f.server(sharedIssuer, "https://granted.example.com/mcp")
	derived := f.server(sharedIssuer, "https://derived.example.com/mcp")
	conflicted := f.server(sharedIssuer, "https://conflicted.example.com/mcp")
	clientA, issuerA := f.gatewayClient(sharedIssuer)
	clientB, issuerB := f.gatewayClient(sharedIssuer)

	f.grant(sharedIssuer, clientA, "https://granted.example.com/mcp/")
	f.derive(granted, issuerA)
	f.derive(derived, issuerB)
	f.grant(sharedIssuer, clientA, "https://conflicted.example.com/mcp")
	f.derive(conflicted, issuerB)

	report := f.run(true)
	require.Equal(t, OutcomeGrant, resultFor(t, report, granted).Outcome)
	require.Equal(t, OutcomeDerived, resultFor(t, report, derived).Outcome)
	require.Equal(t, OutcomeAmbiguous, resultFor(t, report, conflicted).Outcome)
	require.Equal(t, 1, report.Gaps)
	require.Equal(t, []uuid.UUID{clientA}, f.associations(granted))
	require.Equal(t, []uuid.UUID{clientB}, f.associations(derived))
	require.Empty(t, f.associations(conflicted))
}

func TestRunSkipsAnEvidenceWriteOnceTheServerIsConfigured(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	sharedIssuer := f.userIssuer()
	server := f.server(sharedIssuer, "https://granted.example.com/mcp")
	f.server(sharedIssuer, "https://other.example.com/mcp")
	clientA, _ := f.gatewayClient(sharedIssuer)
	clientB, _ := f.gatewayClient(sharedIssuer)
	f.grant(sharedIssuer, clientA, "https://granted.example.com/mcp")

	// An administrator configures the server between the read and the write.
	f.associate(server, clientB, false)

	n, err := New(f.pool).BackfillUpstreamClientAssociation(t.Context(), BackfillUpstreamClientAssociationParams{
		RemoteSessionClientID: clientA,
		McpServerID:           server,
		UserSessionIssuerID:   sharedIssuer,
		RespectRemoved:        true,
		RequireEvidenceMatch:  true,
	})
	require.NoError(t, err)
	require.Zero(t, n, "evidence never adds to a configured server")
	require.Equal(t, []uuid.UUID{clientB}, f.associations(server))
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
	require.Equal(t, OutcomeDetached, resultFor(t, report, linear).Outcome, "a detached match stays detached")
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

func TestRunSkipsAMetadataMatchWriteOnceAnotherClientMatches(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	sharedIssuer := f.userIssuer()
	linear := f.server(sharedIssuer, "https://mcp.linear.example/mcp")
	f.server(sharedIssuer, "https://mcp.other.example/mcp")
	linearClient := f.client(sharedIssuer, "https://linear.example")
	f.protectedResource("https://mcp.linear.example/mcp", "https://linear.example", "https://linear-alt.example")

	rows, err := New(f.pool).ListUpstreamClientBackfillCandidates(t.Context(), ListUpstreamClientBackfillCandidatesParams{
		Cursor:         uuid.Nil,
		OrganizationID: f.orgID,
		PageSize:       10,
	})
	require.NoError(t, err)
	var decided ServerResult
	for _, server := range groupByServer(rows) {
		if server[0].McpServerID == linear {
			decided = Decide(server)
		}
	}
	require.Equal(t, OutcomeMetadataMatch, decided.Outcome)
	require.Equal(t, []uuid.UUID{linearClient}, decided.Associate)

	// A second client the server's metadata names is bound between the read
	// and the write, so a fresh decision would be ambiguous.
	f.client(sharedIssuer, "https://linear-alt.example")

	n, err := New(f.pool).BackfillUpstreamClientAssociation(t.Context(), BackfillUpstreamClientAssociationParams{
		RemoteSessionClientID: linearClient,
		McpServerID:           linear,
		UserSessionIssuerID:   sharedIssuer,
		RespectRemoved:        decided.RespectRemoved,
		RequireMetadataMatch:  true,
	})
	require.NoError(t, err)
	require.Zero(t, n, "a metadata match that is no longer unique proves nothing")
	require.Empty(t, f.associations(linear))
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
			Proxied:               true,
			IssuerServerCount:     issuerServers,
			IssuerHasGateway:      gateway,
			LiveAssociations:      liveAssociations,
			RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: client != uuid.Nil},
			MetadataMatch:         match,
			GrantMatch:            false,
			DerivedMatch:          false,
			PairLive:              live,
			PairRecorded:          recorded,
		}
	}

	unproxied := func(r ListUpstreamClientBackfillCandidatesRow) ListUpstreamClientBackfillCandidatesRow {
		r.RemoteBacked, r.Hosted, r.Proxied = false, true, false
		return r
	}
	granted := func(r ListUpstreamClientBackfillCandidatesRow) ListUpstreamClientBackfillCandidatesRow {
		r.GrantMatch = true
		return r
	}
	derived := func(r ListUpstreamClientBackfillCandidatesRow) ListUpstreamClientBackfillCandidatesRow {
		r.DerivedMatch = true
		return r
	}
	shared := func(client uuid.UUID) ListUpstreamClientBackfillCandidatesRow {
		return row(client, 2, false, false, false, false, 0)
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
		{"a gateway makes the issuer shared", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 1, true, false, false, false, 0)}, OutcomeNoClient, nil},
		{"one metadata match", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, true, false, false, 0), row(clientB, 2, false, false, false, false, 0)}, OutcomeMetadataMatch, []uuid.UUID{clientA}},
		{"two metadata matches are ambiguous", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, true, false, false, 0), row(clientB, 2, false, true, false, false, 0)}, OutcomeAmbiguous, nil},
		{"shared without evidence but configured", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, false, true, true, 1)}, OutcomeConfigured, nil},
		{"shared without evidence", []ListUpstreamClientBackfillCandidatesRow{shared(clientA)}, OutcomeNoClient, nil},
		{"a detached metadata match", []ListUpstreamClientBackfillCandidatesRow{row(clientA, 2, false, true, false, true, 0)}, OutcomeDetached, nil},
		{"one grant", []ListUpstreamClientBackfillCandidatesRow{granted(shared(clientA)), shared(clientB)}, OutcomeGrant, []uuid.UUID{clientA}},
		{"a grant and its own derived issuer", []ListUpstreamClientBackfillCandidatesRow{derived(granted(shared(clientA))), shared(clientB)}, OutcomeGrant, []uuid.UUID{clientA}},
		{"a grant another client's derived issuer contradicts", []ListUpstreamClientBackfillCandidatesRow{granted(shared(clientA)), derived(shared(clientB))}, OutcomeAmbiguous, nil},
		{"two grants", []ListUpstreamClientBackfillCandidatesRow{granted(shared(clientA)), granted(shared(clientB))}, OutcomeAmbiguous, nil},
		{"one derived issuer", []ListUpstreamClientBackfillCandidatesRow{shared(clientA), derived(shared(clientB))}, OutcomeDerived, []uuid.UUID{clientB}},
		{"a detached derived client", []ListUpstreamClientBackfillCandidatesRow{func() ListUpstreamClientBackfillCandidatesRow {
			r := derived(shared(clientA))
			r.PairRecorded = true
			return r
		}()}, OutcomeDetached, nil},
		{"evidence never decides an unproxied server", []ListUpstreamClientBackfillCandidatesRow{unproxied(granted(shared(clientA)))}, OutcomeUnproxied, nil},
		{"shared without evidence but unproxied", []ListUpstreamClientBackfillCandidatesRow{unproxied(row(clientA, 2, false, false, false, false, 0))}, OutcomeUnproxied, nil},
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

func TestRunLeavesOtherOrganizationsAlone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)

	mine := f.server(f.userIssuer(), "https://mine.example.com/mcp")
	theirIssuer := other.userIssuer()
	theirs := other.server(theirIssuer, "https://theirs.example.com/mcp")
	theirClient := other.client(theirIssuer, "https://as-theirs.example.com")

	report := f.run(true)
	for _, result := range report.Results {
		require.NotEqual(t, theirs, result.MCPServerID, "a scoped run decides only its organization's servers")
	}
	require.Equal(t, OutcomeUnbound, resultFor(t, report, mine).Outcome)
	require.Empty(t, other.associations(theirs), "another organization's server is untouched")

	// A server pointing at another organization's issuer proves nothing.
	foreign := f.server(theirIssuer, "https://foreign.example.com/mcp")
	report = f.run(true)
	for _, result := range report.Results {
		require.NotEqual(t, foreign, result.MCPServerID, "a server on a foreign issuer is not a candidate")
	}
	require.Empty(t, f.associations(foreign))
	require.NotContains(t, f.associations(foreign), theirClient)
}

func TestRunSkipsASoleConsumerWriteOnceTheIssuerIsShared(t *testing.T) {
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
	require.Equal(t, OutcomeSoleConsumer, decided.Outcome)

	// A second server joins the issuer between the read and the write.
	f.server(issuer, "https://sibling.example.com/mcp")

	n, err := New(f.pool).BackfillUpstreamClientAssociation(t.Context(), BackfillUpstreamClientAssociationParams{
		RemoteSessionClientID: clientID,
		McpServerID:           server,
		UserSessionIssuerID:   issuer,
		RequireSoleConsumer:   true,
		RespectRemoved:        decided.RespectRemoved,
	})
	require.NoError(t, err)
	require.Zero(t, n, "a binding that is no longer the server's alone proves nothing")
	require.Empty(t, f.associations(server))
}
