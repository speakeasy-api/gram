package mcpconnectionattempts

import (
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
		{"rate near its baseline", ProviderCounts{Attempts: 10, Failures: 5, Organizations: 3, BaselineAttempts: 100, BaselineFailures: 40}, ProviderElevated},
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

func (f *providerFixture) read(window time.Duration, trendDays int) ProviderHealthReport {
	f.t.Helper()
	health := NewProviderHealth(f.conn)
	health.now = func() time.Time { return f.now }
	reports, err := health.Read(f.t.Context(), ProviderHealthQuery{Provider: f.provider, Window: window, TrendDays: trendDays})
	require.NoError(f.t, err)
	require.Len(f.t, reports, 1)
	return reports[0]
}

func TestProviderHealth_OutageAcrossOrganizationsNamesNone(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.squareup.com")
	steady := "org-" + uuid.NewString()
	for range 20 {
		f.write(f.attempt(steady, 3*24*time.Hour), f.step(StepProviderRefresh, ResultRefreshed, true))
	}
	orgs := []string{"org-" + uuid.NewString(), "org-" + uuid.NewString(), "org-" + uuid.NewString()}
	for i := range 6 {
		f.write(f.attempt(orgs[i%len(orgs)], 10*time.Minute), f.step(StepProviderRefresh, CodeProviderUnavailable, true))
	}

	report := f.read(time.Hour, 0)
	require.Equal(t, ProviderOutage, report.Status)
	require.Equal(t, ProviderCounts{Attempts: 6, Failures: 6, Organizations: 3, BaselineAttempts: 20, BaselineFailures: 0}, report.Counts)
	require.Equal(t, []ProviderFailure{{Step: StepProviderRefresh.String(), Reason: CodeProviderUnavailable.String(), Attempts: 6}}, report.Failures)

	printed := fmt.Sprintf("%+v", report)
	for _, org := range append(orgs, steady) {
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
	f.write(f.attempt(org, 2*time.Hour),
		f.step(StepProviderSignIn, ResultSignedIn, false),
		f.step(StepDiscoveryMember, CodeRejectedByMember, false))
	f.write(f.attempt(org, 2*time.Hour), f.step(StepProviderSignIn, ResultAccepted, false))
	f.write(f.attempt(org, 10*time.Minute), f.step(StepProviderSignIn, ResultAccepted, false))

	report := f.read(24*time.Hour, 0)
	require.Equal(t, uint64(1), report.ReconnectLoops)
	require.Equal(t, []ProviderStepCount{{Step: StepProviderSignIn.String(), Attempts: 1}}, report.Abandoned, "an attempt still in its live window is not abandoned")
	require.Equal(t, []ProviderFailure{{Step: StepDiscoveryMember.String(), Reason: CodeRejectedByMember.String(), Attempts: 1}}, report.Failures)
	require.Equal(t, ProviderNormal, report.Status, "nothing failed on the provider's side")
}

func TestProviderHealth_DailyTrendFromTheLongTermTable(t *testing.T) {
	t.Parallel()

	f := newProviderFixture(t, "mcp.box.com")
	day := f.now.Truncate(24*time.Hour).AddDate(0, 0, -2)
	batch, err := f.conn.PrepareBatch(t.Context(), `INSERT INTO `+OutcomesDailyTable+` (day, provider, app, client_family, step, outcome, reason, fixed_by, attempts)`)
	require.NoError(t, err)
	for _, row := range []struct {
		outcome, reason, fixed string
		attempts               uint64
	}{
		{string(OutcomeConnected), "", "", 10},
		{string(OutcomeFailed), CodeProviderUnavailable.String(), string(FixedByRetry), 2},
		{string(OutcomeFailed), CodeProviderUnavailable.String(), "", 1},
		{string(OutcomeAbandoned), "", "", 3},
	} {
		require.NoError(t, batch.Append(day, f.provider, AppDCR.key, "", StepProviderRefresh.String(), row.outcome, row.reason, row.fixed, row.attempts))
	}
	require.NoError(t, batch.Send())

	report := f.read(time.Hour, 7)
	require.Equal(t, ProviderQuiet, report.Status)
	require.Len(t, report.Daily, 1)
	require.True(t, day.Equal(report.Daily[0].Day))
	require.Equal(t, ProviderDay{Day: report.Daily[0].Day, Provider: f.provider, Attempts: 16, Failed: 3, Abandoned: 3, Fixed: 2}, report.Daily[0])
}

func TestProviderHealth_RefusesUnboundedReads(t *testing.T) {
	t.Parallel()

	health := NewProviderHealth(nil)
	_, err := health.Read(t.Context(), ProviderHealthQuery{Provider: "", Window: OutageBaseline + time.Hour, TrendDays: 0})
	require.Error(t, err)
	_, err = health.Read(t.Context(), ProviderHealthQuery{Provider: "", Window: time.Hour, TrendDays: ProviderHealthMaxTrendDays + 1})
	require.Error(t, err)
}
