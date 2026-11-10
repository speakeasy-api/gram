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
// The demo organization is left out of both. Status reads provider-side failure codes
// only, so an outage that only leaves people abandoned on the provider's sign-in page
// is not detected; abandons are reported per step but not judged. Customer outage messages can later
// make a status-only read (Window: OutageWindow, TrendDays: 0) behind a short cache;
// until then only staff see it.

const (
	// OutageWindow is the recent stretch judged: attempts that ended in it. An attempt
	// with a final row ends then; one that stops without one ends when its live window
	// does, so a failure on an in-attempt step reaches status one to two hours after it
	// happened. Counting live rows instead would read retried faults as outages.
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

	// EndingProvider is the provider on the row that decided how the attempt ended, empty when that row names none.
	EndingProvider string `ch:"ending_provider"`

	// AttributedProvider is the provider on the row that decided how the attempt
	// ended, else its earliest provider, so a gateway attempt that checks several
	// members is counted against the member that failed.
	AttributedProvider string `ch:"attributed_provider"`

	// ReconnectLoop says a needs-sign-in or rejected row came after a provider sign-in.
	ReconnectLoop bool `ch:"reconnect_loop"`
}

// providerAttemptsQuery reduces each attempt that started in a range and reached a
// provider; filter narrows it after the reduction. The ending row is picked with the
// same tie-breaks as the ending columns.
func providerAttemptsQuery(filter string) string {
	return `
	SELECT` + attemptEndingColumns + `,
		toString(if(countIf(is_final) > 0,
			argMinIf(provider_host, (event_time, step, outcome, reason), is_final),
			argMax(provider_host, (event_time, step, reason)))) AS ending_provider,
		if(ending_provider != '', ending_provider, provider) AS attributed_provider,
		toBool(countIf(step = 'provider_sign_in' AND step_result = 'signed_in') > 0
			AND maxIf(event_time, reason IN ('rejected_by_member', 'needs_sign_in')) > minIf(event_time, step = 'provider_sign_in' AND step_result = 'signed_in')) AS reconnect_loop
	FROM ` + eventsTable + `
	WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
		AND NOT has(?, organization_id)
	GROUP BY organization_id, attempt_id
	HAVING attributed_provider != ''` + filter
}

// providerDetailsQuery groups attempts that ended in a range by provider and ending,
// with End's rules: the final row decides; otherwise, once the live window is over,
// the last row's reason means failed (blocked or declined for those codes) and no
// reason means abandoned. The range keeps only attempts whose live window is over.
func providerDetailsQuery(filter string) string {
	return `
	SELECT attributed_provider AS detail_provider,
		multiIf(has_final, final_outcome, last_reason = '', 'abandoned', has(?, last_reason), 'not_failed', 'failed') AS detail_outcome,
		if(has_final, final_step, last_step) AS detail_step,
		if(has_final, final_reason, last_reason) AS detail_reason,
		sum(weight) AS detail_attempts,
		sumIf(weight, reconnect_loop) AS detail_loops
	FROM (` + providerAttemptsQuery(filter) + `)
	WHERE if(has_final, final_at, started_at + toIntervalSecond(?)) BETWEEN fromUnixTimestamp64Nano(?) AND fromUnixTimestamp64Nano(?)
	GROUP BY detail_provider, detail_outcome, detail_step, detail_reason`
}

// providerDetail is one row of providerDetailsQuery.
type providerDetail struct {
	// Provider is the raw attributed host.
	Provider string `ch:"detail_provider"`

	// Outcome is how the attempts ended.
	Outcome string `ch:"detail_outcome"`

	// Step is where they ended.
	Step string `ch:"detail_step"`

	// Reason is the ending reason.
	Reason string `ch:"detail_reason"`

	// Attempts is their weight.
	Attempts uint64 `ch:"detail_attempts"`

	// Loops is the weight of reconnect loops among them.
	Loops uint64 `ch:"detail_loops"`
}

// notFailedReasons are codes that end an attempt without a final row as blocked or declined, not failed.
func notFailedReasons() []string {
	var codes []string
	for code, meaning := range catalog {
		if meaning.Outcome == OutcomeBlocked || meaning.Outcome == OutcomeDeclined {
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)
	return codes
}

// Read returns each provider's health, busiest in the last hour first. Organizations stay counts.
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

	// Every known provider is reported, quiet when it has no rows, so silence is visible.
	tallies := map[string]*providerTally{}
	if query.Provider != "" {
		tallies[query.Provider] = newProviderTally()
	} else {
		for host := range knownProviderHosts {
			tallies[host] = newProviderTally()
		}
	}
	tally := func(provider string) *providerTally {
		t, ok := tallies[provider]
		if !ok {
			t = newProviderTally()
			tallies[provider] = t
		}
		return t
	}
	keep := func(host string) (string, bool) {
		provider := ProviderLabel(host)
		return provider, query.Provider == "" || provider == query.Provider
	}

	if err := h.readStatus(ctx, now, query.Provider, keep, tally); err != nil {
		return nil, err
	}
	if err := h.readDetails(ctx, now, now.Add(-query.Window), query.Provider, keep, tally); err != nil {
		return nil, err
	}
	if err := h.readBaseline(ctx, now, query.Provider, keep, tally); err != nil {
		return nil, err
	}
	var daily map[string][]ProviderDay
	if query.TrendDays > 0 {
		var err error
		daily, err = h.readDaily(ctx, now, query, keep)
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
		return cmp.Or(cmp.Compare(b.Counts.Attempts, a.Counts.Attempts), cmp.Compare(a.Provider, b.Provider))
	})
	return reports, nil
}

// providerFilter pushes a known provider down. other is every unlisted host, so it is filtered after.
func providerFilter(provider string, args []any) (string, []any) {
	if provider == "" || provider == LabelOther {
		return "", args
	}
	return " AND attributed_provider = ?", append(args, provider)
}

// readStatus counts the attempts that ended in the last OutageWindow, per attempt so
// organizations can be counted. It scans one live window further back, for attempts
// that started earlier and ended inside it.
func (h *ProviderHealth) readStatus(ctx context.Context, now time.Time, provider string, keep func(string) (string, bool), tally func(string) *providerTally) error {
	statusFrom := now.Add(-OutageWindow)
	filter, args := providerFilter(provider, []any{statusFrom.Add(-AttemptLiveWindow).UnixNano(), now.UnixNano(), ExcludedOrganizations()})
	rows, err := h.conn.Query(ctx, providerAttemptsQuery(filter), args...)
	if err != nil {
		return fmt.Errorf("query provider status: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var a providerAttempt
		if err := rows.ScanStruct(&a); err != nil {
			return fmt.Errorf("scan provider attempt: %w", err)
		}
		if label, ok := keep(a.AttributedProvider); ok {
			tally(label).add(a, now, statusFrom)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read provider status: %w", err)
	}
	return nil
}

// readDetails adds failures, abandons and reconnect loops for attempts that ended
// in the detail window, grouped in ClickHouse.
func (h *ProviderHealth) readDetails(ctx context.Context, now, detailFrom time.Time, provider string, keep func(string) (string, bool), tally func(string) *providerTally) error {
	filter, args := providerFilter(provider, []any{notFailedReasons(), detailFrom.Add(-AttemptLiveWindow).UnixNano(), now.UnixNano(), ExcludedOrganizations()})
	args = append(args, int64(AttemptLiveWindow/time.Second), detailFrom.UnixNano(), now.UnixNano())
	rows, err := h.conn.Query(ctx, providerDetailsQuery(filter), args...)
	if err != nil {
		return fmt.Errorf("query provider details: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var d providerDetail
		if err := rows.ScanStruct(&d); err != nil {
			return fmt.Errorf("scan provider detail: %w", err)
		}
		label, ok := keep(d.Provider)
		if !ok {
			continue
		}
		t := tally(label)
		t.loops += d.Loops
		switch Outcome(d.Outcome) {
		case OutcomeFailed:
			t.failures[[2]string{d.Step, d.Reason}] += d.Attempts
		case OutcomeAbandoned:
			t.abandoned[d.Step] += d.Attempts
		case OutcomeConnected, OutcomeBlocked, OutcomeDeclined:
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read provider details: %w", err)
	}
	return nil
}

// endedAt is when an attempt ended: its final row, or the end of its live window.
// The daily baseline counts attempts by the day they started, so an attempt that
// starts just before midnight UTC can land on a different day there than here.
func (a providerAttempt) endedAt() time.Time {
	if a.HasFinal {
		return a.FinalAt
	}
	return a.StartedAt.Add(AttemptLiveWindow)
}

// add counts one attempt that ended in the status window. A live attempt can still
// recover, so it is left out until it ends.
func (t *providerTally) add(a providerAttempt, now, statusFrom time.Time) {
	ended := a.endedAt()
	if ended.After(now) || ended.Before(statusFrom) {
		return
	}
	weight := max(a.Weight, 1)
	t.counts.Attempts += weight
	if meaning, ok := MeaningOf(a.End(now).Reason); ok && meaning.WhoFixes == WhoProvider {
		t.counts.Failures += weight
		t.organizations[a.OrganizationID] = struct{}{}
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
// Stored providers are labelled again, so a row that broke the daily job's rule
// still cannot name a tenant host here.
func (h *ProviderHealth) readBaseline(ctx context.Context, now time.Time, provider string, keep func(string) (string, bool), tally func(string) *providerTally) error {
	today := now.Truncate(24 * time.Hour)
	where, args := dailyProviderFilter(provider, "day >= toDate(?) AND day < toDate(?)", []any{today.AddDate(0, 0, -outageBaselineDays), today})
	rows, err := h.conn.Query(ctx, `SELECT provider, reason, sum(attempts) AS reason_attempts
	FROM `+OutcomesDailyTable+` WHERE `+where+` AND provider != ''
	GROUP BY provider, reason`, args...)
	if err != nil {
		return fmt.Errorf("query provider baseline: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var stored, reason string
		var attempts uint64
		if err := rows.Scan(&stored, &reason, &attempts); err != nil {
			return fmt.Errorf("scan provider baseline: %w", err)
		}
		label, ok := keep(stored)
		if !ok || label == "" {
			continue
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

// dailyProviderFilter pushes a known provider down. other also gathers any stray
// stored host, so it is filtered after labelling.
func dailyProviderFilter(provider, where string, args []any) (string, []any) {
	if provider == "" || provider == LabelOther {
		return where, args
	}
	return where + " AND provider = ?", append(args, provider)
}

// readDaily returns the daily trend per provider label, oldest first, labelling stored providers again.
func (h *ProviderHealth) readDaily(ctx context.Context, now time.Time, query ProviderHealthQuery, keep func(string) (string, bool)) (map[string][]ProviderDay, error) {
	today := now.Truncate(24 * time.Hour)
	from := today.AddDate(0, 0, -query.TrendDays)
	where, args := dailyProviderFilter(query.Provider, "day >= toDate(?) AND day < toDate(?)", []any{from, today})
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
		label, ok := keep(day.Provider)
		if !ok || label == "" {
			continue
		}
		day.Provider = label
		days := daily[label]
		if n := len(days); n > 0 && days[n-1].Day.Equal(day.Day) {
			days[n-1].Attempts += day.Attempts
			days[n-1].Failed += day.Failed
			days[n-1].Abandoned += day.Abandoned
			days[n-1].Fixed += day.Fixed
			continue
		}
		daily[label] = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read daily provider outcomes: %w", err)
	}
	return daily, nil
}
