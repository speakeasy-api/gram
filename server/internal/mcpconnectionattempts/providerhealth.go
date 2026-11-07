package mcpconnectionattempts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Provider health compares each known provider's recent provider-side failures,
// across every organization, with its own recent past. It is computed when asked
// and stored nowhere. Customer outage messages can later call Read behind a short
// cache; until then only staff see it.

const (
	// OutageWindow is the recent stretch judged: long enough to hold a few attempts
	// at today's volume, short enough that an outage shows within the hour.
	OutageWindow = time.Hour

	// OutageBaseline is the provider's own past the window is compared with, so a
	// weekly pattern is in it.
	OutageBaseline = 7 * 24 * time.Hour

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

	// providerHealthReadSeconds lifts the connection's 60-second limit for eight days of events.
	providerHealthReadSeconds = 300

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

	// ProviderElevated meets all but one.
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
	// Attempts reached the provider in the window.
	Attempts uint64

	// Failures ended on a provider-side reason in the window.
	Failures uint64

	// Organizations had a provider-side failure in the window.
	Organizations int

	// BaselineAttempts reached the provider in the baseline.
	BaselineAttempts uint64

	// BaselineFailures ended on a provider-side reason in the baseline.
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
	met := 0
	for _, ok := range []bool{
		c.Failures >= OutageMinFailures,
		c.Organizations >= OutageMinOrganizations,
		c.Rate() >= max(OutageRateMultiple*baseline, baseline+OutageRateIncrease),
	} {
		if ok {
			met++
		}
	}
	switch met {
	case 3:
		return ProviderOutage
	case 2:
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

	// ReconnectLoops are window attempts that signed in to the provider and still
	// ended needing sign-in or rejected by the server.
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
	conn driver.Conn
	now  func() time.Time
}

// NewProviderHealth reads through conn, which needs SELECT on the events and daily tables.
func NewProviderHealth(conn driver.Conn) *ProviderHealth {
	return &ProviderHealth{conn: conn, now: time.Now}
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

// Read returns each provider's health, busiest first. Organizations stay counts.
func (h *ProviderHealth) Read(ctx context.Context, query ProviderHealthQuery) ([]ProviderHealthReport, error) {
	if query.Window <= 0 || query.Window > OutageBaseline {
		return nil, fmt.Errorf("provider health window must be within %s", OutageBaseline)
	}
	if query.TrendDays < 0 || query.TrendDays > ProviderHealthMaxTrendDays {
		return nil, fmt.Errorf("provider health trend must be at most %d days", ProviderHealthMaxTrendDays)
	}
	now := h.now().UTC()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": providerHealthReadSeconds,
		"max_memory_usage":   providerHealthReadMemoryBytes,
	}))

	statusFrom := now.Add(-OutageWindow)
	detailFrom := now.Add(-query.Window)
	tallies := map[string]*providerTally{}
	filter := AttemptFilter{
		StartedFrom:            statusFrom.Add(-OutageBaseline),
		StartedBefore:          now,
		OrganizationID:         "",
		ExcludeOrganizationIDs: nil,
		ProjectID:              uuid.Nil,
		ServerID:               "",
		PeopleFailingFrom:      time.Time{},
		PeopleFailingBefore:    time.Time{},
	}
	err := ReadAttempts(ctx, h.conn, filter, func(a AttemptSummary) {
		provider := ProviderLabel(a.ProviderHost)
		if provider == "" || (query.Provider != "" && provider != query.Provider) {
			return
		}
		tally, ok := tallies[provider]
		if !ok {
			tally = &providerTally{counts: noProviderCounts, organizations: map[string]struct{}{}, failures: map[[2]string]uint64{}, abandoned: map[string]uint64{}, loops: 0}
			tallies[provider] = tally
		}
		tally.add(a, now, statusFrom, detailFrom)
	})
	if err != nil {
		return nil, fmt.Errorf("read provider attempts: %w", err)
	}

	var daily map[string][]ProviderDay
	if query.TrendDays > 0 {
		daily, err = h.readDaily(ctx, now, query)
		if err != nil {
			return nil, err
		}
	}

	reports := make([]ProviderHealthReport, 0, len(tallies))
	for provider, tally := range tallies {
		reports = append(reports, tally.report(provider, daily[provider]))
	}
	for provider, days := range daily {
		if _, ok := tallies[provider]; !ok {
			reports = append(reports, (&providerTally{counts: noProviderCounts, organizations: nil, failures: nil, abandoned: nil, loops: 0}).report(provider, days))
		}
	}
	slices.SortFunc(reports, func(a, b ProviderHealthReport) int {
		return cmp.Or(cmp.Compare(b.Counts.Attempts+b.Counts.BaselineAttempts, a.Counts.Attempts+a.Counts.BaselineAttempts), cmp.Compare(a.Provider, b.Provider))
	})
	return reports, nil
}

func (t *providerTally) add(a AttemptSummary, now, statusFrom, detailFrom time.Time) {
	weight := max(a.Weight, 1)
	reason := a.LastReason
	if a.HasFinal {
		reason = a.FinalReason
	}
	meaning, known := MeaningOf(reason)
	providerFault := known && meaning.WhoFixes == WhoProvider

	if a.StartedAt.Before(statusFrom) {
		t.counts.BaselineAttempts += weight
		if providerFault {
			t.counts.BaselineFailures += weight
		}
	} else {
		t.counts.Attempts += weight
		if providerFault {
			t.counts.Failures += weight
			t.organizations[a.OrganizationID] = struct{}{}
		}
	}

	if a.StartedAt.Before(detailFrom) {
		return
	}
	ending := a.End(now)
	switch {
	case ending.Outcome == OutcomeAbandoned && !now.Before(a.StartedAt.Add(AttemptLiveWindow)):
		t.abandoned[ending.Step] += weight
	case ending.Outcome == OutcomeFailed:
		t.failures[[2]string{ending.Step, ending.Reason}] += weight
	}
	if a.SignedIn && (isReconnectReason(a.LastReason) || (a.HasFinal && isReconnectReason(a.FinalReason))) {
		t.loops += weight
	}
}

// isReconnectReason is a reason that sends a person who just signed in back to sign in.
func isReconnectReason(reason string) bool {
	return reason == CodeRejectedByMember.String() || reason == CodeNeedsSignIn.String()
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
