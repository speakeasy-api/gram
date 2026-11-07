package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Provider health compares each known provider's provider-side failures in the last
// hour, across every organization, with its own last seven closed days. It is
// computed when asked and stored nowhere. Only ended attempts count, in the window as
// in the baseline, so a provider fault someone is still retrying is not yet a failure.
// The demo organization is left out of both. Customer outage messages can later
// make a status-only read (Window: OutageWindow, TrendDays: 0) behind a short cache;
// until then only staff see it.

const (
	// OutageWindow is the recent stretch judged: long enough to hold a few attempts
	// at today's volume, short enough that an outage shows within the hour.
	OutageWindow = time.Hour

	// OutageBaseline is the provider's own past the window is compared with, so a
	// weekly pattern is in it. It is read from closed days of the daily table, so it
	// lags by up to a day, and a provider used by fewer than three organizations on a
	// day is counted under other that day, leaving it a smaller or no baseline.
	OutageBaseline = 7 * 24 * time.Hour

	// outageBaselineDays is OutageBaseline in whole days.
	outageBaselineDays = 7

	// OutageMinFailures is how many provider-side failed attempts the window needs.
	// Prod sees a few a week, so fewer is noise.
	OutageMinFailures = 5

	// OutageMinOrganizations is how many organizations must hit provider-side
	// failures in the window, so one organization's broken setup never reads as an outage.
	OutageMinOrganizations = 3

	// OutageRateMultiple is how many times the baseline failure rate the window must reach.
	OutageRateMultiple = 5

	// OutageRateIncrease is how far above the baseline failure rate the window must
	// be, so a provider that almost never fails needs a real jump, not 0% to 1%.
	OutageRateIncrease = 0.20

	// ProviderHealthMaxTrendDays is how far back the daily trend reaches: the daily table's 13 months.
	ProviderHealthMaxTrendDays = 395

	// providerHealthTopFailures bounds the failure list per provider.
	providerHealthTopFailures = 5

	// providerHealthReadSeconds bounds one read: at most seven days of events, reduced per attempt.
	providerHealthReadSeconds = 60

	// providerHealthReadMemoryBytes caps one read.
	providerHealthReadMemoryBytes = 1 << 30 // 1 GiB

	// OutcomesDailyTable keeps daily connection outcomes for 13 months, with no organization.
	OutcomesDailyTable = "mcp_connection_outcomes_daily"
)

// ProviderStatus is a provider's health in the last OutageWindow.
type ProviderStatus string

const (
	// ProviderOutage meets every outage threshold.
	ProviderOutage ProviderStatus = "outage"

	// ProviderElevated meets the rate threshold and one of the two count thresholds.
	ProviderElevated ProviderStatus = "elevated"

	// ProviderNormal had attempts and is not elevated.
	ProviderNormal ProviderStatus = "normal"

	// ProviderQuiet had no attempts.
	ProviderQuiet ProviderStatus = "quiet"

	// ProviderNotChecked is the other label: many providers in one, so never judged.
	ProviderNotChecked ProviderStatus = "not_checked"
)

// ProviderCounts are a provider's attempts, weighted by the repeats they stand for.
type ProviderCounts struct {
	// Attempts reached the provider and ended in the window.
	Attempts uint64

	// Failures ended on a provider-side reason in the window.
	Failures uint64

	// Organizations had a provider-side failure in the window.
	Organizations int

	// BaselineAttempts reached the provider and ended in the baseline days.
	BaselineAttempts uint64

	// BaselineFailures ended on a provider-side reason in the baseline days.
	BaselineFailures uint64
}

// Rate is the window's provider-side failure rate.
func (c ProviderCounts) Rate() float64 { return ratio(c.Failures, c.Attempts) }

// BaselineRate is the baseline's provider-side failure rate.
func (c ProviderCounts) BaselineRate() float64 { return ratio(c.BaselineFailures, c.BaselineAttempts) }

// Status applies the outage thresholds.
func (c ProviderCounts) Status() ProviderStatus {
	if c.Attempts == 0 {
		return ProviderQuiet
	}
	baseline := c.BaselineRate()
	if c.Rate() < max(OutageRateMultiple*baseline, baseline+OutageRateIncrease) {
		return ProviderNormal
	}
	enoughFailures := c.Failures >= OutageMinFailures
	enoughOrganizations := c.Organizations >= OutageMinOrganizations
	switch {
	case enoughFailures && enoughOrganizations:
		return ProviderOutage
	case enoughFailures || enoughOrganizations:
		return ProviderElevated
	default:
		return ProviderNormal
	}
}

func ratio(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// noProviderCounts is a provider with nothing counted.
var noProviderCounts = ProviderCounts{Attempts: 0, Failures: 0, Organizations: 0, BaselineAttempts: 0, BaselineFailures: 0}

// ProviderHealthQuery selects what Read returns.
type ProviderHealthQuery struct {
	// Provider keeps one provider label, or every one when empty.
	Provider string

	// Window is how far back failures, reconnect loops and abandons reach, at most
	// OutageBaseline. Status always uses OutageWindow.
	Window time.Duration

	// TrendDays is how many whole days of daily outcomes to return, none when zero.
	TrendDays int
}

// ProviderHealthReport is one provider's health. It holds counts only: no
// organization, project, person or server.
type ProviderHealthReport struct {
	// Provider is a known vendor host or other.
	Provider string

	// Status is the provider's health in the last OutageWindow.
	Status ProviderStatus

	// Counts back Status.
	Counts ProviderCounts

	// Failures are the most common ways attempts ended failed in the window, any party at fault.
	Failures []ProviderFailure

	// ReconnectLoops are ended window attempts that signed in at the provider, then
	// needed sign-in again or were rejected by the server.
	ReconnectLoops uint64

	// Abandoned counts window attempts past their live window that ended with nothing, by step.
	Abandoned []ProviderStepCount

	// Daily is the daily trend, oldest first.
	Daily []ProviderDay
}

// ProviderFailure is how often attempts ended on one step and reason.
type ProviderFailure struct {
	// Step is where the attempts ended.
	Step string

	// Reason is the catalog code.
	Reason string

	// Attempts ended this way.
	Attempts uint64
}

// ProviderStepCount is a count at one step.
type ProviderStepCount struct {
	// Step is the step.
	Step string

	// Attempts counted there.
	Attempts uint64
}

// ProviderDay is one day from the daily table. Its ch tags are aliases, which must not shadow the raw columns.
type ProviderDay struct {
	// Day is the UTC day.
	Day time.Time `ch:"day"`

	// Provider is the label, other on days fewer than three organizations used it.
	Provider string `ch:"provider"`

	// Attempts started that day.
	Attempts uint64 `ch:"day_attempts"`

	// Failed ended failed.
	Failed uint64 `ch:"day_failed"`

	// Abandoned ended with nothing.
	Abandoned uint64 `ch:"day_abandoned"`

	// Fixed failed and were fixed later.
	Fixed uint64 `ch:"day_fixed"`
}

// ProviderHealth reads provider health across every organization.
type ProviderHealth struct {
	logger *slog.Logger
	conn   driver.Conn
	now    func() time.Time
}

// NewProviderHealth reads through conn, which needs SELECT on the events and daily tables.
func NewProviderHealth(logger *slog.Logger, conn driver.Conn) *ProviderHealth {
	return &ProviderHealth{logger: logger.With(attr.SlogComponent("provider_health")), conn: conn, now: time.Now}
}

// ValidProviderLabel reports whether label is a known vendor host or other.
func ValidProviderLabel(label string) bool {
	return label != "" && ProviderLabel(label) == label
}

// providerTally accumulates one provider's attempts.
type providerTally struct {
	counts        ProviderCounts
	organizations map[string]struct{}
	failures      map[[2]string]uint64
	abandoned     map[string]uint64
	loops         uint64
}

func newProviderTally() *providerTally {
	return &providerTally{counts: noProviderCounts, organizations: map[string]struct{}{}, failures: map[[2]string]uint64{}, abandoned: map[string]uint64{}, loops: 0}
}

// providerAttempt is one attempt reduced in ClickHouse to what provider health needs.
type providerAttempt struct {
	AttemptSummary

	// ReconnectLoop says a needs-sign-in or rejected row came after a provider sign-in.
	ReconnectLoop bool `ch:"reconnect_loop"`
}

// providerAttemptColumns adds the reconnect loop to the ending columns.
const providerAttemptColumns = attemptEndingColumns + `,
	toBool(countIf(step = 'provider_sign_in' AND step_result = 'signed_in') > 0
		AND maxIf(event_time, reason IN ('rejected_by_member', 'needs_sign_in')) > minIf(event_time, step = 'provider_sign_in' AND step_result = 'signed_in')) AS reconnect_loop`

// Read returns each provider's health, busiest first. Organizations stay counts.
// A status-only caller passes Window: OutageWindow and TrendDays: 0.
func (h *ProviderHealth) Read(ctx context.Context, query ProviderHealthQuery) ([]ProviderHealthReport, error) {
	if query.Window <= 0 || query.Window > OutageBaseline {
		return nil, fmt.Errorf("provider health window must be within %s", OutageBaseline)
	}
	if query.TrendDays < 0 || query.TrendDays > ProviderHealthMaxTrendDays {
		return nil, fmt.Errorf("provider health trend must be at most %d days", ProviderHealthMaxTrendDays)
	}
	reports, err := h.read(ctx, query)
	if err != nil {
		h.logger.ErrorContext(ctx, "read provider health", attr.SlogError(err))
		return nil, err
	}
	return reports, nil
}

func (h *ProviderHealth) read(ctx context.Context, query ProviderHealthQuery) ([]ProviderHealthReport, error) {
	now := h.now().UTC()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": providerHealthReadSeconds,
		"max_memory_usage":   providerHealthReadMemoryBytes,
	}))

	statusFrom := now.Add(-OutageWindow)
	detailFrom := min(statusFrom.UnixNano(), now.Add(-query.Window).UnixNano())
	tallies := map[string]*providerTally{}
	tally := func(provider string) *providerTally {
		t, ok := tallies[provider]
		if !ok {
			t = newProviderTally()
			tallies[provider] = t
		}
		return t
	}

	providerFilter, args := "", []any{detailFrom, now.UnixNano(), ExcludedOrganizations()}
	// other is every unlisted host, so only a known host is pushed down.
	if query.Provider != "" && query.Provider != LabelOther {
		providerFilter, args = " AND provider = ?", append(args, query.Provider)
	}
	rows, err := h.conn.Query(ctx, `SELECT`+providerAttemptColumns+` FROM `+eventsTable+`
	WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
		AND NOT has(?, organization_id)
	GROUP BY organization_id, attempt_id
	HAVING provider != ''`+providerFilter, args...)
	if err != nil {
		return nil, fmt.Errorf("query provider attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var a providerAttempt
		if err := rows.ScanStruct(&a); err != nil {
			return nil, fmt.Errorf("scan provider attempt: %w", err)
		}
		provider := ProviderLabel(a.ProviderHost)
		if query.Provider != "" && provider != query.Provider {
			continue
		}
		tally(provider).add(a, now, statusFrom)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read provider attempts: %w", err)
	}

	if err := h.readBaseline(ctx, now, query.Provider, tally); err != nil {
		return nil, err
	}
	var daily map[string][]ProviderDay
	if query.TrendDays > 0 {
		daily, err = h.readDaily(ctx, now, query)
		if err != nil {
			return nil, err
		}
		for provider := range daily {
			tally(provider)
		}
	}

	reports := make([]ProviderHealthReport, 0, len(tallies))
	for provider, t := range tallies {
		reports = append(reports, t.report(provider, daily[provider]))
	}
	slices.SortFunc(reports, func(a, b ProviderHealthReport) int {
		return cmp.Or(cmp.Compare(b.Counts.Attempts+b.Counts.BaselineAttempts, a.Counts.Attempts+a.Counts.BaselineAttempts), cmp.Compare(a.Provider, b.Provider))
	})
	return reports, nil
}

// add counts one ended attempt. A live attempt can still recover, so it is left out.
func (t *providerTally) add(a providerAttempt, now, statusFrom time.Time) {
	if !a.HasFinal && now.Before(a.StartedAt.Add(AttemptLiveWindow)) {
		return
	}
	weight := max(a.Weight, 1)
	ending := a.End(now)
	if !a.StartedAt.Before(statusFrom) {
		t.counts.Attempts += weight
		if meaning, ok := MeaningOf(ending.Reason); ok && meaning.WhoFixes == WhoProvider {
			t.counts.Failures += weight
			t.organizations[a.OrganizationID] = struct{}{}
		}
	}
	switch ending.Outcome {
	case OutcomeAbandoned:
		t.abandoned[ending.Step] += weight
	case OutcomeFailed:
		t.failures[[2]string{ending.Step, ending.Reason}] += weight
	case OutcomeConnected, OutcomeBlocked, OutcomeDeclined:
	}
	if a.ReconnectLoop {
		t.loops += weight
	}
}

func (t *providerTally) report(provider string, daily []ProviderDay) ProviderHealthReport {
	t.counts.Organizations = len(t.organizations)
	status := t.counts.Status()
	if provider == LabelOther {
		status = ProviderNotChecked
	}
	failures := make([]ProviderFailure, 0, len(t.failures))
	for key, n := range t.failures {
		failures = append(failures, ProviderFailure{Step: key[0], Reason: key[1], Attempts: n})
	}
	slices.SortFunc(failures, func(a, b ProviderFailure) int {
		return cmp.Or(cmp.Compare(b.Attempts, a.Attempts), cmp.Compare(a.Step, b.Step), cmp.Compare(a.Reason, b.Reason))
	})
	abandoned := make([]ProviderStepCount, 0, len(t.abandoned))
	for step, n := range t.abandoned {
		abandoned = append(abandoned, ProviderStepCount{Step: step, Attempts: n})
	}
	slices.SortFunc(abandoned, func(a, b ProviderStepCount) int {
		return cmp.Or(cmp.Compare(b.Attempts, a.Attempts), cmp.Compare(a.Step, b.Step))
	})
	return ProviderHealthReport{
		Provider:       provider,
		Status:         status,
		Counts:         t.counts,
		Failures:       failures[:min(len(failures), providerHealthTopFailures)],
		ReconnectLoops: t.loops,
		Abandoned:      abandoned,
		Daily:          daily,
	}
}

// readBaseline adds the baseline days' ended attempts and provider-side failures.
func (h *ProviderHealth) readBaseline(ctx context.Context, now time.Time, provider string, tally func(string) *providerTally) error {
	today := now.Truncate(24 * time.Hour)
	where, args := "day >= toDate(?) AND day < toDate(?)", []any{today.AddDate(0, 0, -outageBaselineDays), today}
	if provider != "" {
		where, args = where+" AND provider = ?", append(args, provider)
	}
	rows, err := h.conn.Query(ctx, `SELECT provider, reason, sum(attempts) AS reason_attempts
	FROM `+OutcomesDailyTable+` WHERE `+where+` AND provider != ''
	GROUP BY provider, reason`, args...)
	if err != nil {
		return fmt.Errorf("query provider baseline: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var label, reason string
		var attempts uint64
		if err := rows.Scan(&label, &reason, &attempts); err != nil {
			return fmt.Errorf("scan provider baseline: %w", err)
		}
		t := tally(label)
		t.counts.BaselineAttempts += attempts
		if meaning, ok := MeaningOf(reason); ok && meaning.WhoFixes == WhoProvider {
			t.counts.BaselineFailures += attempts
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read provider baseline: %w", err)
	}
	return nil
}

func (h *ProviderHealth) readDaily(ctx context.Context, now time.Time, query ProviderHealthQuery) (map[string][]ProviderDay, error) {
	today := now.Truncate(24 * time.Hour)
	from := today.AddDate(0, 0, -query.TrendDays)
	where, args := "day >= toDate(?) AND day < toDate(?)", []any{from, today}
	if query.Provider != "" {
		where, args = where+" AND provider = ?", append(args, query.Provider)
	}
	rows, err := h.conn.Query(ctx, `SELECT day, provider,
		sum(attempts) AS day_attempts,
		sumIf(attempts, outcome = 'failed') AS day_failed,
		sumIf(attempts, outcome = 'abandoned') AS day_abandoned,
		sumIf(attempts, fixed_by != '') AS day_fixed
	FROM `+OutcomesDailyTable+` WHERE `+where+`
	GROUP BY day, provider ORDER BY day, provider`, args...)
	if err != nil {
		return nil, fmt.Errorf("query daily provider outcomes: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	daily := map[string][]ProviderDay{}
	for rows.Next() {
		var day ProviderDay
		if err := rows.ScanStruct(&day); err != nil {
			return nil, fmt.Errorf("scan daily provider outcome: %w", err)
		}
		if day.Provider == "" {
			continue
		}
		daily[day.Provider] = append(daily[day.Provider], day)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read daily provider outcomes: %w", err)
	}
	return daily, nil
}
