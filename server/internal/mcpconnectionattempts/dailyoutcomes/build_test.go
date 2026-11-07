package dailyoutcomes

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The long-term table has no tenant, so each test owns the days it writes. Days are
// counted back from today and stay inside the rawSafeDays (29) the job reads.
const (
	daysAgoOutcomes    = 25
	daysAgoRerun       = 23
	daysAgoFixedBy     = 20
	daysAgoPlanOld     = 15
	daysAgoExpiring    = 11
	daysAgoEmpty       = 10
	daysAgoMismatch    = 9
	daysAgoCrossOrg    = 19
	daysAgoPrivacy     = 8
	daysAgoPlanDemo    = 6
	daysAgoPlanMissing = 4
	daysAgoPlanRecent  = 3
)

const catalogApp = "https://claude.ai/oauth/claude-code-client-metadata"

type fixture struct {
	t     *testing.T
	b     *Builder
	ch    driver.Conn
	db    *pgxpool.Pool
	org   string
	now   time.Time
	today time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "dailyoutcomes")
	require.NoError(t, err)
	now := time.Now().UTC()
	return &fixture{t: t, b: NewBuilder(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, ch), ch: ch, db: db, org: "org-" + uuid.NewString(), now: now, today: StartOfDay(now)}
}

func (f *fixture) day(daysAgo int) time.Time {
	return f.today.AddDate(0, 0, -daysAgo)
}

func (f *fixture) attempt(serverID uuid.UUID, app mcpconnectionattempts.App, started time.Time) mcpconnectionattempts.Attempt {
	scope := mcpconnectionattempts.ServerScope(f.org, uuid.New(), serverID, app)
	return mcpconnectionattempts.AttemptAt(uuid.New(), started, scope)
}

func (f *fixture) row(a mcpconnectionattempts.Attempt, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, final bool, after time.Duration, opts ...mcpconnectionattempts.Option) mcpconnectionattempts.Row {
	f.t.Helper()
	row, err := mcpconnectionattempts.RowAt(a, step, what, final, a.StartedAt().Add(after), opts...)
	require.NoError(f.t, err)
	return row
}

func (f *fixture) write(rows ...mcpconnectionattempts.Row) {
	f.t.Helper()
	require.NoError(f.t, mcpconnectionattempts.AppendRows(f.t.Context(), f.ch, rows))
}

func (f *fixture) build(day time.Time) Result {
	f.t.Helper()
	result, err := f.b.BuildDay(f.t.Context(), day, f.now)
	require.NoError(f.t, err)
	return result
}

// stored reads a day as "provider|app|client|step|outcome|reason|fixed_by" to attempts.
func (f *fixture) stored(table string, day time.Time) map[string]uint64 {
	f.t.Helper()
	rows, err := f.ch.Query(f.t.Context(), `SELECT provider, app, client_family, step, outcome, reason, fixed_by, attempts FROM `+table+` WHERE day = toDate(?)`, day.Format(time.DateOnly))
	require.NoError(f.t, err)
	defer func() { require.NoError(f.t, rows.Close()) }()
	out := map[string]uint64{}
	for rows.Next() {
		var provider, app, client, step, outcome, reason, fixed string
		var attempts uint64
		require.NoError(f.t, rows.Scan(&provider, &app, &client, &step, &outcome, &reason, &fixed, &attempts))
		out[strings.Join([]string{provider, app, client, step, outcome, reason, fixed}, "|")] += attempts
	}
	require.NoError(f.t, rows.Err())
	return out
}

func (f *fixture) insertStored(table string, day time.Time, key string, attempts uint64) {
	f.t.Helper()
	parts := strings.Split(key, "|")
	require.Len(f.t, parts, 7)
	batch, err := f.ch.PrepareBatch(f.t.Context(), `INSERT INTO `+table+` (day, provider, app, client_family, step, outcome, reason, fixed_by, attempts)`)
	require.NoError(f.t, err)
	require.NoError(f.t, batch.Append(day, parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6], attempts))
	require.NoError(f.t, batch.Send())
}

func (f *fixture) audit(org, subjectID, action, actorType, actorID string, internal bool, at time.Time) {
	f.t.Helper()
	q := auditrepo.New(f.db)
	inserted, err := q.InsertAuditLog(f.t.Context(), auditrepo.InsertAuditLogParams{
		OrganizationID: org, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ActorID: actorID, ActorType: actorType,
		ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""), Action: action,
		SubjectID: subjectID, SubjectType: "mcp_server", SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""),
		BeforeSnapshot: nil, AfterSnapshot: nil, Metadata: nil, ActingSurface: conv.ToPGTextEmpty(""), ActingClientID: conv.ToPGTextEmpty(""), IsInternal: internal,
	})
	require.NoError(f.t, err)
	require.NoError(f.t, q.UpdateAuditLogCreatedAtForTesting(f.t.Context(), auditrepo.UpdateAuditLogCreatedAtForTestingParams{
		CreatedAt: conv.ToPGTimestamptz(at), Ids: []uuid.UUID{inserted.ID},
	}))
}

func TestBuildDayDecidesHowAttemptsEnded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoOutcomes)
	server := uuid.New()
	app := mcpconnectionattempts.AppOf(catalogApp)

	connected := f.attempt(server, app, day.Add(time.Hour))
	connectedRow := f.row(connected, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute)
	connectedRow.ClientFamily = mcpconnectionattempts.FamilyClaudeCode

	afterFinal := f.attempt(server, app, day.Add(2*time.Hour))
	stopped := f.attempt(server, app, day.Add(3*time.Hour))
	abandoned := f.attempt(server, app, day.Add(4*time.Hour))
	otherApp := f.attempt(server, mcpconnectionattempts.AppOtherMetadataURL, day.Add(5*time.Hour))

	repeated := f.attempt(server, mcpconnectionattempts.AppDCR, day.Add(6*time.Hour))
	repeatedRow := f.row(repeated, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, 0)
	repeatedRow.Repeats = 4

	midnight := f.attempt(server, app, day.Add(23*time.Hour+50*time.Minute))
	blocked := f.attempt(server, app, day.Add(7*time.Hour))

	demo := mcpconnectionattempts.AttemptAt(uuid.New(), day.Add(time.Hour), mcpconnectionattempts.ServerScope(constants.DemoOrganizationID, uuid.New(), server, app))

	f.write(
		connectedRow,
		f.row(afterFinal, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied, true, time.Minute),
		f.row(afterFinal, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn, false, 2*time.Minute),
		f.row(stopped, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0),
		f.row(stopped, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidClient, false, time.Minute, mcpconnectionattempts.Provider("https://acme.okta.com/oauth2/default")),
		f.row(abandoned, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0, mcpconnectionattempts.Provider("https://accounts.google.com")),
		f.row(otherApp, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute),
		repeatedRow,
		f.row(midnight, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, 0),
		f.row(midnight, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, 30*time.Minute),
		f.row(demo, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch, true, time.Minute),
		f.row(blocked, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn, false, time.Minute),
	)

	result := f.build(day)
	require.True(t, result.Swapped)
	require.Equal(t, map[string]uint64{
		// One organization, so the vendor names fold into other.
		"|other|claude_code|inbound_token|connected||":                        1,
		"|other||inbound_token|connected||":                                   1,
		"|other||provider_sign_in|declined|provider_denied|":                  1,
		"other|other||provider_sign_in|failed|provider_oauth_invalid_client|": 1,
		"other|other||inbound_authorize|abandoned||":                          1,
		"|other_metadata_url||inbound_token|connected||":                      1,
		"|dcr||mcp_request|failed|invalid_bearer_token|":                      5,
		"|other||discovery_member|blocked|needs_sign_in|":                     1,
	}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
	require.Empty(t, f.stored(stagingTable, day), "staging is cleared after the swap")
}

func TestBuildDayNamesWhatFixedEachFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoFixedBy)
	app := mcpconnectionattempts.AppDCR
	subject := urn.NewUserSubject("user-" + uuid.NewString())
	person := mcpconnectionattempts.Subject(&subject)

	fail := func(server uuid.UUID, at time.Time, opts ...mcpconnectionattempts.Option) mcpconnectionattempts.Row {
		a := f.attempt(server, app, at)
		return f.row(a, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, 0, append(opts, person)...)
	}
	succeed := func(server uuid.UUID, at time.Time, signIn bool) []mcpconnectionattempts.Row {
		a := f.attempt(server, app, at)
		rows := []mcpconnectionattempts.Row{}
		if signIn {
			rows = append(rows, f.row(a, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultSignedIn, false, 0, person))
		}
		return append(rows, f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute, person))
	}

	retry, reconnect, admin, system, staff, looked, unfixed, lookahead, tooLate := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := day.Add(time.Hour)
	systemFailure := fail(system, at)
	systemFailure.AuthClientID = "auth-client-" + uuid.NewString()

	rows := []mcpconnectionattempts.Row{
		fail(retry, at), fail(reconnect, at), fail(admin, at), systemFailure, fail(staff, at), fail(looked, at), fail(unfixed, at), fail(lookahead, at), fail(tooLate, at),
	}
	rows = append(rows, succeed(retry, at.Add(time.Hour), false)...)
	rows = append(rows, succeed(reconnect, at.Add(time.Hour), true)...)
	rows = append(rows, succeed(admin, at.Add(time.Hour), true)...)
	rows = append(rows, succeed(system, at.Add(time.Hour), true)...)
	rows = append(rows, succeed(staff, at.Add(time.Hour), false)...)
	rows = append(rows, succeed(looked, at.Add(time.Hour), false)...)
	rows = append(rows, succeed(lookahead, at.Add(3*24*time.Hour), false)...)
	rows = append(rows, succeed(tooLate, at.Add(mcpconnectionattempts.FixLookahead+time.Hour), false)...)
	f.write(rows...)

	f.audit(f.org, admin.String(), "mcp-server:update", "api_key", "key_admin", false, at.Add(30*time.Minute))
	f.audit(f.org, systemFailure.AuthClientID, "mcp-server:update", "user", "system", false, at.Add(30*time.Minute))
	f.audit(f.org, staff.String(), "mcp-server:update", "user", "user_staff", true, at.Add(30*time.Minute))
	f.audit(f.org, looked.String(), "platform-mcp-diagnostics:user_status_read", "user", "user_admin", false, at.Add(30*time.Minute))
	f.audit(f.org, retry.String(), "mcp-server:update", "user", "user_admin", false, at.Add(-time.Hour))

	f.build(day)
	require.Equal(t, map[string]uint64{
		"|dcr||inbound_token|connected||":                             6,
		"|dcr||mcp_request|failed|invalid_bearer_token|retry":         3,
		"|dcr||mcp_request|failed|invalid_bearer_token|reconnect":     1,
		"|dcr||mcp_request|failed|invalid_bearer_token|admin_change":  1,
		"|dcr||mcp_request|failed|invalid_bearer_token|system_change": 2,
		"|dcr||mcp_request|failed|invalid_bearer_token|":              2,
	}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
}

func TestBuildDayRerunGivesTheSameResult(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoRerun)
	a := f.attempt(uuid.New(), mcpconnectionattempts.AppDCR, day.Add(time.Hour))
	b := f.attempt(uuid.New(), mcpconnectionattempts.AppDCR, day.Add(2*time.Hour))
	f.write(
		f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute),
		f.row(b, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch, true, time.Minute),
	)

	f.build(day)
	first := f.stored(mcpconnectionattempts.OutcomesDailyTable, day)
	require.Len(t, first, 2)
	f.build(day)
	require.Equal(t, first, f.stored(mcpconnectionattempts.OutcomesDailyTable, day), "a rerun replaces the day with the same rows")

	// A run that died after staging leaves the stored day alone, and the next run cleans up.
	f.insertStored(stagingTable, day, "|junk||inbound_token|connected||", 99)
	require.Equal(t, first, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
	f.build(day)
	require.Equal(t, first, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
	require.Empty(t, f.stored(stagingTable, day))

	// A run that died after the swap but before clearing staging leaves a complete day.
	rows, err := f.b.outcomes(t.Context(), day, f.now)
	require.NoError(t, err)
	for _, row := range rows {
		f.insertStored(stagingTable, day, strings.Join([]string{row.Provider, row.App, row.ClientFamily, row.Step, row.Outcome, row.Reason, row.FixedBy}, "|"), row.Attempts)
	}
	f.build(day)
	require.Equal(t, first, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
	require.Empty(t, f.stored(stagingTable, day))
}

func TestBuildDayLeavesADayWithNoAttemptsAsStored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoEmpty)
	earlier := map[string]uint64{"|dcr||inbound_token|connected||": 3}
	f.insertStored(mcpconnectionattempts.OutcomesDailyTable, day, "|dcr||inbound_token|connected||", 3)

	result := f.build(day)
	require.False(t, result.Swapped)
	require.Equal(t, earlier, f.stored(mcpconnectionattempts.OutcomesDailyTable, day), "raw rows that expired must not wipe the day")
}

func TestPublishAbortsWithoutSwappingOnACountMismatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoMismatch)
	f.insertStored(mcpconnectionattempts.OutcomesDailyTable, day, "|dcr||inbound_token|connected||", 3)
	rows := []DayRow{{Provider: "", App: "dcr", ClientFamily: "", Step: "inbound_token", Outcome: "connected", Reason: "", FixedBy: "", Attempts: 7}}

	want := totalsOf(rows)
	want.rows++
	result, err := f.b.publish(t.Context(), day, rows, want)
	require.ErrorIs(t, err, ErrStagedMismatch)
	require.False(t, result.Swapped)
	require.Equal(t, map[string]uint64{"|dcr||inbound_token|connected||": 3}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
}

func TestBuildDayRefusesADayThatIsStillOpen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	yesterday := f.today.AddDate(0, 0, -1)
	_, err := f.b.BuildDay(t.Context(), yesterday, yesterday.Add(24*time.Hour+DayCloseDelay-time.Second))
	require.ErrorIs(t, err, ErrDayOpen)
}

func TestDaysToBuild(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.b.backfillStart = f.today.AddDate(0, 0, -rawSafeDays)
	server := uuid.New()
	write := func(day time.Time, org string) {
		a := mcpconnectionattempts.AttemptAt(uuid.New(), day.Add(time.Hour), mcpconnectionattempts.ServerScope(org, uuid.New(), server, mcpconnectionattempts.AppDCR))
		f.write(f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute))
	}
	old, missing, recent, demoOnly := f.day(daysAgoPlanOld), f.day(daysAgoPlanMissing), f.day(daysAgoPlanRecent), f.day(daysAgoPlanDemo)
	write(old, f.org)
	write(missing, f.org)
	write(recent, f.org)
	write(demoOnly, constants.DemoOrganizationID)
	f.build(old)
	f.build(recent)

	days, err := f.b.DaysToBuild(t.Context(), f.now)
	require.NoError(t, err)
	require.True(t, slices.IsSortedFunc(days, func(a, b time.Time) int { return a.Compare(b) }))
	require.Contains(t, days, missing, "a day the table lacks is backfilled")
	require.Contains(t, days, recent, "a day in its fix lookahead is rebuilt")
	require.NotContains(t, days, old, "a settled day is left alone")
	require.NotContains(t, days, demoOnly, "the demo organization is left out")

	limited := NewBuilder(testenv.NewLogger(t), testenv.NewMeterProvider(t), f.db, f.ch)
	limited.backfillStart = recent
	days, err = limited.DaysToBuild(t.Context(), f.now)
	require.NoError(t, err)
	require.NotContains(t, days, missing, "nothing before the backfill start")
}

func TestBuildDayKeepsVendorNamesOnlyWhenSeveralOrganizationsUseThem(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoPrivacy)
	write := func(org, provider, appURL string) {
		a := mcpconnectionattempts.AttemptAt(uuid.New(), day.Add(time.Hour), mcpconnectionattempts.ServerScope(org, uuid.New(), uuid.New(), mcpconnectionattempts.AppOf(appURL)))
		f.write(f.row(a, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued, true, time.Minute, mcpconnectionattempts.Provider(provider)))
	}
	for range minOrgsPerLabel {
		write("org-"+uuid.NewString(), "https://accounts.google.com", catalogApp)
	}
	write(f.org, "https://github.com", "https://zed.dev/oauth/client-metadata.json")

	f.build(day)
	require.Equal(t, map[string]uint64{
		"accounts.google.com|claude.ai||inbound_token|connected||": uint64(minOrgsPerLabel),
		"other|other||inbound_token|connected||":                   1,
	}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
}

func TestBuildDayPairsOnlyWithinTheOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoCrossOrg)
	otherOrg := "org-" + uuid.NewString()
	subject := urn.NewUserSubject("user-" + uuid.NewString())
	person := mcpconnectionattempts.Subject(&subject)
	at := day.Add(time.Hour)
	inOrg := func(org string, server uuid.UUID, started time.Time, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened) mcpconnectionattempts.Row {
		a := mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.ServerScope(org, uuid.New(), server, mcpconnectionattempts.AppDCR))
		return f.row(a, step, what, true, 0, person)
	}

	// Same person and server id succeed only in another organization.
	alone, shared := uuid.New(), uuid.New()
	f.write(
		inOrg(f.org, alone, at, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken),
		inOrg(otherOrg, alone, at.Add(time.Hour), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued),
		// Another organization's admin changes a server with the same id between this failure and its success.
		inOrg(f.org, shared, at, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken),
		inOrg(f.org, shared, at.Add(time.Hour), mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued),
	)
	f.audit(otherOrg, shared.String(), "mcp-server:update", "user", "user_other_admin", false, at.Add(30*time.Minute))
	f.audit(otherOrg, alone.String(), "mcp-server:update", "user", "user_other_admin", false, at.Add(30*time.Minute))

	f.build(day)
	require.Equal(t, map[string]uint64{
		"|dcr||inbound_token|connected||":                     2,
		"|dcr||mcp_request|failed|invalid_bearer_token|":      1,
		"|dcr||mcp_request|failed|invalid_bearer_token|retry": 1,
	}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
}

func TestBuildDayRefusesADayWhoseRawEventsMayBeExpiring(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	day := f.day(daysAgoExpiring)
	f.insertStored(mcpconnectionattempts.OutcomesDailyTable, day, "|dcr||inbound_token|connected||", 3)
	f.b.clock = func() time.Time { return day.AddDate(0, 0, rawSafeDays+1) }

	_, err := f.b.BuildDay(t.Context(), day, f.now)
	require.ErrorIs(t, err, ErrDayExpiring)
	require.Equal(t, map[string]uint64{"|dcr||inbound_token|connected||": 3}, f.stored(mcpconnectionattempts.OutcomesDailyTable, day))
}
