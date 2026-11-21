package problems

import (
	"context"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func logsOff(context.Context, string) (bool, error) { return false, nil }

type readFixture struct {
	t   *testing.T
	ch  driver.Conn
	db  *pgxpool.Pool
	now time.Time
}

func newReadFixture(t *testing.T) *readFixture {
	t.Helper()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return &readFixture{t: t, ch: ch, db: db, now: time.Now().UTC()}
}

// fail writes a rejected saved key for a new person on scope, ago before now.
func (f *readFixture) fail(scope mcpconnectionattempts.Scope, ago time.Duration, opts ...mcpconnectionattempts.Option) urn.SessionSubject {
	f.t.Helper()
	p := urn.NewUserSubject("user-" + uuid.NewString())
	f.failAs(p, scope, ago, mcpconnectionattempts.CodeStaticKeyRejected, opts...)
	return p
}

// failAs writes a request that failed with code for p on scope, ago before now.
func (f *readFixture) failAs(p urn.SessionSubject, scope mcpconnectionattempts.Scope, ago time.Duration, code mcpconnectionattempts.Code, opts ...mcpconnectionattempts.Option) {
	f.t.Helper()
	at := f.now.Add(-ago)
	row, err := mcpconnectionattempts.RowAt(mcpconnectionattempts.AttemptAt(uuid.New(), at, scope), mcpconnectionattempts.StepMCPRequest, code, true, at, append(opts, mcpconnectionattempts.Subject(&p))...)
	require.NoError(f.t, err)
	require.NoError(f.t, mcpconnectionattempts.AppendRows(f.t.Context(), f.ch, []mcpconnectionattempts.Row{row}))
}

func (f *readFixture) read(org string, recordedSince time.Time) []Problem {
	f.t.Helper()
	got, err := Read(f.t.Context(), f.ch, f.db, mcpconnectionattemptstest.LogsOn, org, recordedSince, f.now)
	require.NoError(f.t, err)
	return got
}

func TestRead_LogsOffReturnsTheError(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	_, err := Read(t.Context(), f.ch, f.db, logsOff, "org-"+uuid.NewString(), time.Time{}, f.now)
	require.ErrorIs(t, err, mcpconnectionattempts.ErrLogsOff)
}

func TestRead_RejectsAnEmptyOrganization(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	_, err := Read(t.Context(), f.ch, f.db, mcpconnectionattemptstest.LogsOn, "", time.Time{}, f.now)
	require.Error(t, err)
}

// Another organization failing on the same server id is not counted.
func TestRead_GroupsOneOrganizationsFailures(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	orgA, orgB, project, server := "org-"+uuid.NewString(), "org-"+uuid.NewString(), uuid.New(), uuid.New()
	f.fail(mcpconnectionattempts.ServerScope(orgA, project, server, mcpconnectionattempts.AppDCR), time.Hour)
	f.fail(mcpconnectionattempts.ServerScope(orgB, project, server, mcpconnectionattempts.AppDCR), time.Hour)
	require.Empty(t, f.read(orgA, time.Time{}), "one person in each organization makes no problem")

	f.fail(mcpconnectionattempts.ServerScope(orgA, project, server, mcpconnectionattempts.AppDCR), time.Hour)
	got := f.read(orgA, time.Time{})
	require.Len(t, got, 1)
	require.Equal(t, RuleSameServer, got[0].Rule)
	require.Equal(t, 2, got[0].People)
	require.Equal(t, orgA, got[0].OrganizationID)
}

func TestRead_StartsWhereRecordingStarted(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	org, project, server := "org-"+uuid.NewString(), uuid.New(), uuid.New()
	for range MinPeople {
		f.fail(mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR), 3*time.Hour)
	}
	require.Len(t, f.read(org, time.Time{}), 1)
	require.Empty(t, f.read(org, f.now.Add(-time.Hour)), "failures from before recording started are not read")
}

func TestRead_CountsAnEarlierRunFromWhenRecordingStarted(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	org, project, server := "org-"+uuid.NewString(), uuid.New(), uuid.New()
	scope := mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR)
	p := f.fail(scope, 30*time.Hour)
	f.failAs(p, scope, 30*time.Minute, mcpconnectionattempts.CodeStaticKeyRejected)

	got := f.read(org, time.Time{})
	require.Len(t, got, 1)
	require.Equal(t, RuleOnePerson, got[0].Rule)

	recorded := f.now.Add(-2 * time.Hour)
	require.Empty(t, f.read(org, recorded), "the run counts from when recording started, two hours ago")
}

// A failure last seen before recording started is not counted, even when it became known as failed later.
func TestRead_DropsAFailureLastSeenBeforeRecordingStarted(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	org, project, server := "org-"+uuid.NewString(), uuid.New(), uuid.New()
	for range MinPeople {
		p := urn.NewUserSubject("user-" + uuid.NewString())
		started := f.now.Add(-100 * time.Minute)
		row, err := mcpconnectionattempts.RowAt(mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR)),
			mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected, false, started.Add(time.Minute), mcpconnectionattempts.Subject(&p))
		require.NoError(t, err)
		require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), f.ch, []mcpconnectionattempts.Row{row}))
	}
	require.Len(t, f.read(org, time.Time{}), 1)
	require.Empty(t, f.read(org, f.now.Add(-time.Hour)), "known as failed 40 minutes ago, last seen failing 99 minutes ago")
}

// Old outages retried just after recording started count from then, so their grace has not passed.
func TestRead_AppliesGraceFromWhenRecordingStarted(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	org, project, server := "org-"+uuid.NewString(), uuid.New(), uuid.New()
	scope := mcpconnectionattempts.ServerScope(org, project, server, mcpconnectionattempts.AppDCR)
	for range MinPeople {
		p := urn.NewUserSubject("user-" + uuid.NewString())
		f.failAs(p, scope, 3*time.Hour, mcpconnectionattempts.CodeProviderServerError)
		f.failAs(p, scope, 9*time.Minute, mcpconnectionattempts.CodeProviderServerError)
	}
	require.Len(t, f.read(org, time.Time{}), 1)
	require.Empty(t, f.read(org, f.now.Add(-10*time.Minute)), "recording started 10 minutes ago, inside the 30 minute grace")
}

type orgFixture struct {
	organizationID string
	project        uuid.UUID
}

func seedOrg(t *testing.T, db *pgxpool.Pool) orgFixture {
	t.Helper()
	q := testrepo.New(db)
	org := "org-" + uuid.NewString()
	require.NoError(t, q.SeedDelegationLoaderOrganizationFixture(t.Context(), testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: org, Slug: org}))
	project, err := q.CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "p", Slug: "p-" + uuid.NewString()[:8], OrganizationID: org})
	require.NoError(t, err)
	return orgFixture{organizationID: org, project: project}
}

// seedServer creates an MCP server proxying url.
func seedServer(t *testing.T, db *pgxpool.Pool, o orgFixture, url string) uuid.UUID {
	t.Helper()
	q := testrepo.New(db)
	remote, server := uuid.New(), uuid.New()
	require.NoError(t, q.CreateRemoteMCPBackendFixture(t.Context(), testrepo.CreateRemoteMCPBackendFixtureParams{ID: remote, ProjectID: o.project, Url: url}))
	require.NoError(t, q.CreateRemoteBackedMCPServerFixture(t.Context(), testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: server, ProjectID: o.project, Slug: conv.ToPGText("srv-" + server.String()[:8]), RemoteMcpServerID: uuid.NullUUID{UUID: remote, Valid: true}, Visibility: "private",
	}))
	return server
}

// attachIssuer creates an issuer owned by owner and stamps it on the server.
func attachIssuer(t *testing.T, db *pgxpool.Pool, o orgFixture, server uuid.UUID, owner, issuer string) {
	t.Helper()
	q := testrepo.New(db)
	id := uuid.New()
	require.NoError(t, q.SeedDelegationLoaderIssuerFixture(t.Context(), testrepo.SeedDelegationLoaderIssuerFixtureParams{
		ID: id, OrganizationID: conv.ToPGText(owner), Slug: "issuer-" + id.String()[:8], Issuer: issuer,
		AuthorizationEndpoint: pgtype.Text{}, TokenEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{},
	}))
	n, err := q.SetMCPServerRemoteSessionIssuerFixture(t.Context(), testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: uuid.NullUUID{UUID: id, Valid: true}, ID: server, ProjectID: o.project})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func TestRead_GroupsAServiceAcrossADirectServerAndAGatewayMember(t *testing.T) {
	t.Parallel()
	f := newReadFixture(t)
	o := seedOrg(t, f.db)
	direct := seedServer(t, f.db, o, "https://mcp.example.com/a")
	memberServer := seedServer(t, f.db, o, "https://MCP.example.com:8443/b")

	down := mcpconnectionattempts.CodeProviderServerError
	f.failAs(urn.NewUserSubject("user-"+uuid.NewString()), mcpconnectionattempts.ServerScope(o.organizationID, o.project, direct, mcpconnectionattempts.AppDCR), time.Hour, down)
	f.failAs(urn.NewUserSubject("user-"+uuid.NewString()), mcpconnectionattempts.GatewayScope(o.organizationID, o.project, uuid.New(), mcpconnectionattempts.AppDCR), time.Hour, down, mcpconnectionattempts.Member(memberServer))

	got := f.read(o.organizationID, time.Time{})
	require.Len(t, got, 1)
	require.Equal(t, RuleSameService, got[0].Rule)
	require.Equal(t, Scope{Kind: ScopeService, Key: "mcp.example.com"}, got[0].Scope)
	require.Equal(t, 2, got[0].People)
}

func TestListServerUpstreams(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	mine, theirs := seedOrg(t, db), seedOrg(t, db)

	withIssuer := seedServer(t, db, mine, "https://mcp.example.com/mcp")
	attachIssuer(t, db, mine, withIssuer, mine.organizationID, "https://auth.example.com")
	noIssuer := seedServer(t, db, mine, "https://plain.example.com/mcp")
	foreignIssuer := seedServer(t, db, mine, "https://foreign.example.com/mcp")
	attachIssuer(t, db, mine, foreignIssuer, theirs.organizationID, "https://auth.other.example.com")
	deleted := seedServer(t, db, mine, "https://deleted.example.com/mcp")
	n, err := repo.New(db).SoftDeleteMCPServerFixture(t.Context(), repo.SoftDeleteMCPServerFixtureParams{ID: deleted, ProjectID: mine.project})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	other := seedServer(t, db, theirs, "https://other.example.com/mcp")
	tunnel, err := repo.New(db).CreateTunneledMCPServerFixture(t.Context(), repo.CreateTunneledMCPServerFixtureParams{ProjectID: mine.project, Name: "tunnel-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	attachIssuer(t, db, mine, tunnel, mine.organizationID, "https://tunnel-auth.example.com")
	bareTunnel, err := repo.New(db).CreateTunneledMCPServerFixture(t.Context(), repo.CreateTunneledMCPServerFixtureParams{ProjectID: mine.project, Name: "tunnel-" + uuid.NewString()[:8]})
	require.NoError(t, err)

	rows, err := repo.New(db).ListServerUpstreams(t.Context(), repo.ListServerUpstreamsParams{
		OrganizationID: mine.organizationID,
		ServerIds:      []uuid.UUID{withIssuer, noIssuer, foreignIssuer, deleted, other, tunnel, bareTunnel},
	})
	require.NoError(t, err)
	got := map[uuid.UUID]Upstream{}
	for _, r := range rows {
		got[r.ID] = Upstream{URL: r.Url.String, Issuer: r.Issuer.String}
	}
	require.Equal(t, map[uuid.UUID]Upstream{
		withIssuer:    {URL: "https://mcp.example.com/mcp", Issuer: "https://auth.example.com"},
		noIssuer:      {URL: "https://plain.example.com/mcp", Issuer: ""},
		foreignIssuer: {URL: "https://foreign.example.com/mcp", Issuer: ""},
		tunnel:        {URL: "", Issuer: "https://tunnel-auth.example.com"},
	}, got, "no deleted server, no other organization's server or issuer, no tunnel without an issuer")
}
