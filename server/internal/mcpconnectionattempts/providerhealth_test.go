package mcpconnectionattempts

import (
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProviderCounts_Status(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		counts ProviderCounts
		want   ProviderStatus
	}{
		{"no attempts", ProviderCounts{}, ProviderQuiet},
		{"every threshold", ProviderCounts{Attempts: 10, Failures: 5, Organizations: 3, BaselineAttempts: 100, BaselineFailures: 1}, ProviderOutage},
		{"one organization", ProviderCounts{Attempts: 10, Failures: 5, Organizations: 1, BaselineAttempts: 100, BaselineFailures: 1}, ProviderElevated},
		{"too few failures", ProviderCounts{Attempts: 4, Failures: 4, Organizations: 3, BaselineAttempts: 100, BaselineFailures: 1}, ProviderElevated},
		{"rate near its baseline", ProviderCounts{Attempts: 10, Failures: 5, Organizations: 3, BaselineAttempts: 100, BaselineFailures: 40}, ProviderNormal},
		{"rate alone", ProviderCounts{Attempts: 2, Failures: 2, Organizations: 2, BaselineAttempts: 100, BaselineFailures: 1}, ProviderNormal},
		{"no baseline", ProviderCounts{Attempts: 10, Failures: 5, Organizations: 3, BaselineAttempts: 0, BaselineFailures: 0}, ProviderOutage},
		{"one threshold", ProviderCounts{Attempts: 100, Failures: 5, Organizations: 1, BaselineAttempts: 100, BaselineFailures: 5}, ProviderNormal},
		{"healthy", ProviderCounts{Attempts: 50, Failures: 0, Organizations: 0, BaselineAttempts: 300, BaselineFailures: 1}, ProviderNormal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.counts.Status())
		})
	}
}

func TestValidProviderLabel(t *testing.T) {
	t.Parallel()

	require.True(t, ValidProviderLabel("accounts.google.com"))
	require.True(t, ValidProviderLabel(LabelOther))
	require.False(t, ValidProviderLabel("acme.okta.com"), "a tenant host is never a label")
	require.False(t, ValidProviderLabel(""))
}

// providerFixture writes attempts for one provider. The ClickHouse database is
// shared by every test in the package, so each test uses its own provider.
type providerFixture struct {
	t        *testing.T
	conn     driver.Conn
	provider string
	now      time.Time
}

func newProviderFixture(t *testing.T, provider string) *providerFixture {
	t.Helper()
	return &providerFixture{t: t, conn: clickhouseConn(t), provider: provider, now: time.Now().UTC()}
}

func (f *providerFixture) attempt(org string, startedAgo time.Duration) Attempt {
	return AttemptAt(uuid.New(), f.now.Add(-startedAgo), ServerScope(org, uuid.New(), uuid.New(), AppDCR))
}

// write records rows in order, one second apart.
func (f *providerFixture) write(attempt Attempt, steps ...func(Attempt, time.Time) (Row, error)) {
	f.t.Helper()
	rows := make([]Row, 0, len(steps))
	for i, step := range steps {
		row, err := step(attempt, attempt.StartedAt().Add(time.Duration(i)*time.Second))
		require.NoError(f.t, err)
		rows = append(rows, row)
	}
	require.NoError(f.t, AppendRows(f.t.Context(), f.conn, rows))
}

func (f *providerFixture) step(step Step, what Happened, final bool) func(Attempt, time.Time) (Row, error) {
	return func(a Attempt, at time.Time) (Row, error) {
		return RowAt(a, step, what, final, at, Provider("https://"+f.provider+"/oauth"))
	}
}

// writeDay adds attempts to the daily table, daysAgo whole days before today, and returns that day.
func (f *providerFixture) writeDay(daysAgo int, outcome, reason, fixed string, attempts uint64) time.Time {
	f.t.Helper()
	day := f.now.Truncate(24*time.Hour).AddDate(0, 0, -daysAgo)
	batch, err := f.conn.PrepareBatch(f.t.Context(), `INSERT INTO `+OutcomesDailyTable+` (day, provider, app, client_family, step, outcome, reason, fixed_by, attempts)`)
	require.NoError(f.t, err)
	require.NoError(f.t, batch.Append(day, f.provider, AppDCR.key, "", StepProviderRefresh.String(), outcome, reason, fixed, attempts))
	require.NoError(f.t, batch.Send())
	return day
}

func (f *providerFixture) read(window time.Duration, trendDays int) ProviderHealthReport {
	f.t.Helper()
	health := NewProviderHealth(testenv.NewLogger(f.t), f.conn)
	health.now = func() time.Time { return f.now }
	reports, err := health.Read(f.t.Context(), ProviderHealthQuery{Provider: f.provider, Window: window, TrendDays: trendDays})
	require.NoError(f.t, err)
	require.Len(f.t, reports, 1)
	return reports[0]
}

func TestProviderHealth_OutageAcrossOrganizationsNamesNone(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.squareup.com")
	f.writeDay(3, string(OutcomeConnected), "", "", 20)
	orgs := []string{"org-" + uuid.NewString(), "org-" + uuid.NewString(), "org-" + uuid.NewString()}
	for i := range 6 {
		f.write(f.attempt(orgs[i%len(orgs)], 10*time.Minute), f.step(StepProviderRefresh, CodeProviderUnavailable, true))
	}

	report := f.read(time.Hour, 0)
	require.Equal(t, ProviderOutage, report.Status)
	require.Equal(t, ProviderCounts{Attempts: 6, Failures: 6, Organizations: 3, BaselineAttempts: 20, BaselineFailures: 0}, report.Counts)
	require.Equal(t, []ProviderFailure{{Step: StepProviderRefresh.String(), Reason: CodeProviderUnavailable.String(), Attempts: 6}}, report.Failures)

	printed := fmt.Sprintf("%+v", report)
	for _, org := range orgs {
		require.NotContains(t, printed, org)
	}
}

func TestProviderHealth_OneOrganizationIsNotAnOutage(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.paypal.com")
	org := "org-" + uuid.NewString()
	for range 6 {
		f.write(f.attempt(org, 10*time.Minute), f.step(StepProviderRefresh, CodeProviderServerError, true))
	}

	report := f.read(time.Hour, 0)
	require.Equal(t, ProviderElevated, report.Status)
	require.Equal(t, 1, report.Counts.Organizations)
}

func TestProviderHealth_CountsReconnectLoopsAndAbandons(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.canva.com")
	org := "org-" + uuid.NewString()
	f.write(f.attempt(org, 3*time.Hour),
		f.step(StepProviderSignIn, ResultSignedIn, false),
		f.step(StepDiscoveryMember, CodeRejectedByMember, false))
	f.write(f.attempt(org, 3*time.Hour), f.step(StepProviderSignIn, ResultAccepted, false))
	f.write(f.attempt(org, 10*time.Minute), f.step(StepProviderSignIn, ResultAccepted, false))
	f.write(f.attempt(org, 3*time.Hour),
		f.step(StepDiscoveryMember, CodeNeedsSignIn, false),
		f.step(StepProviderSignIn, ResultSignedIn, true))

	report := f.read(24*time.Hour, 0)
	require.Equal(t, uint64(1), report.ReconnectLoops, "a first connect that signs in after needing to is not a loop")
	require.Equal(t, []ProviderStepCount{{Step: StepProviderSignIn.String(), Attempts: 1}}, report.Abandoned, "an attempt still in its live window is not abandoned")
	require.Equal(t, []ProviderFailure{{Step: StepDiscoveryMember.String(), Reason: CodeRejectedByMember.String(), Attempts: 1}}, report.Failures)
	require.Equal(t, ProviderQuiet, report.Status, "nothing ended in the last hour")
}

func TestProviderHealth_DailyTrendFromTheLongTermTable(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.box.com")
	day := f.writeDay(2, string(OutcomeConnected), "", "", 10)
	f.writeDay(2, string(OutcomeFailed), CodeProviderUnavailable.String(), string(FixedByRetry), 2)
	f.writeDay(2, string(OutcomeFailed), CodeProviderUnavailable.String(), "", 1)
	f.writeDay(2, string(OutcomeAbandoned), "", "", 3)

	report := f.read(time.Hour, 7)
	require.Equal(t, ProviderQuiet, report.Status)
	require.Equal(t, uint64(16), report.Counts.BaselineAttempts, "the baseline comes from the same table")
	require.Equal(t, uint64(3), report.Counts.BaselineFailures)
	require.Len(t, report.Daily, 1)
	require.True(t, day.Equal(report.Daily[0].Day))
	require.Equal(t, ProviderDay{Day: report.Daily[0].Day, Provider: f.provider, Attempts: 16, Failed: 3, Abandoned: 3, Fixed: 2}, report.Daily[0])
}

func TestProviderHealth_RefusesUnboundedReads(t *testing.T) {
	t.Parallel()

	health := NewProviderHealth(testenv.NewLogger(t), nil)
	_, err := health.Read(t.Context(), ProviderHealthQuery{Provider: "", Window: OutageBaseline + time.Hour, TrendDays: 0})
	require.Error(t, err)
	_, err = health.Read(t.Context(), ProviderHealthQuery{Provider: "", Window: time.Hour, TrendDays: ProviderHealthMaxTrendDays + 1})
	require.Error(t, err)
}

func TestProviderHealth_LiveAttemptIsNotCounted(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.figma.com")
	for range 6 {
		f.write(f.attempt("org-"+uuid.NewString(), 10*time.Minute), f.step(StepProviderRefresh, CodeProviderUnavailable, false))
	}

	report := f.read(time.Hour, 0)
	require.Equal(t, ProviderQuiet, report.Status, "a provider fault still being retried is not a failure yet")
	require.Zero(t, report.Counts.Failures)
}

func TestProviderHealth_LeavesOutTheDemoOrganization(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.zapier.com")
	for range 6 {
		f.write(f.attempt(constants.DemoOrganizationID, 10*time.Minute), f.step(StepProviderRefresh, CodeProviderUnavailable, true))
	}

	health := NewProviderHealth(testenv.NewLogger(t), f.conn)
	reports, err := health.Read(t.Context(), ProviderHealthQuery{Provider: f.provider, Window: time.Hour, TrendDays: 0})
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, ProviderQuiet, reports[0].Status)
	require.Equal(t, noProviderCounts, reports[0].Counts)
}

func TestProviderHealth_ReportsEveryKnownProvider(t *testing.T) {
	t.Parallel()

	health := NewProviderHealth(testenv.NewLogger(t), clickhouseConn(t))
	reports, err := health.Read(t.Context(), ProviderHealthQuery{Provider: "", Window: time.Hour, TrendDays: 0})
	require.NoError(t, err)

	byProvider := map[string]ProviderHealthReport{}
	for _, report := range reports {
		byProvider[report.Provider] = report
	}
	for host := range knownProviderHosts {
		require.Contains(t, byProvider, host)
	}
	require.Equal(t, ProviderQuiet, byProvider["connect.stripe.com"].Status, "a provider with no rows is quiet, not missing")
	for i := 1; i < len(reports); i++ {
		require.GreaterOrEqual(t, reports[i-1].Counts.Attempts, reports[i].Counts.Attempts, "busiest in the last hour first")
	}
}

func TestProviderHealth_StoppedAttemptCountsOnceItsLiveWindowEnds(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.monday.com")
	orgs := []string{"org-" + uuid.NewString(), "org-" + uuid.NewString(), "org-" + uuid.NewString()}
	for i := range 6 {
		f.write(f.attempt(orgs[i%len(orgs)], 30*time.Minute), f.step(StepProviderRefresh, CodeProviderUnavailable, false))
	}

	before := f.read(time.Hour, 0)
	require.Equal(t, ProviderQuiet, before.Status, "still live, so still recoverable")

	f.now = f.now.Add(45 * time.Minute)
	after := f.read(time.Hour, 0)
	require.Equal(t, ProviderOutage, after.Status)
	require.Equal(t, ProviderCounts{Attempts: 6, Failures: 6, Organizations: 3, BaselineAttempts: 0, BaselineFailures: 0}, after.Counts, "counted once, when its live window ended")
}

func TestProviderHealth_AttemptEndingInTheWindowCounts(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "auth.monday.com")
	attempt := f.attempt("org-"+uuid.NewString(), 90*time.Minute)
	f.write(attempt, f.step(StepProviderSignIn, ResultSignedIn, false), func(a Attempt, _ time.Time) (Row, error) {
		return RowAt(a, StepProviderToken, CodeProviderUnavailable, true, a.StartedAt().Add(80*time.Minute), Provider("https://"+f.provider+"/oauth"))
	})

	report := f.read(time.Hour, 0)
	require.Equal(t, uint64(1), report.Counts.Attempts, "it started before the window but ended inside it")
	require.Equal(t, uint64(1), report.Counts.Failures)
}
