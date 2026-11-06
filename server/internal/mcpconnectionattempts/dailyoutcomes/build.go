package dailyoutcomes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// DayCloseDelay is how long after midnight UTC a day's attempts are left to end:
	// an attempt started just before midnight stays live up to mcpconnectionattempts.AttemptLiveWindow, and
	// its rows can reach ClickHouse late. Day D is never built before 01:30 on D+1.
	DayCloseDelay = 90 * time.Minute

	// rawSafeDays is the oldest day built from raw events. Raw rows live 30 days and
	// a part drops once all its rows expire, so a day near the limit may be partly
	// gone. Two days of margin keep every built day complete.
	rawSafeDays = 28

	// scheduleInterval is how often the job runs. A day is rebuilt once more after
	// it settles, so that build sees the whole recovery window.
	scheduleInterval = 24 * time.Hour

	// day24h is one UTC day.
	day24h = 24 * time.Hour

	mainTable    = "mcp_connection_outcomes_daily"
	stagingTable = "mcp_connection_outcomes_daily_staging"
)

// BackfillStart is the first day with raw connection events.
var BackfillStart = time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)

// ErrDayOpen means the day can still gain attempts, so building it now would be wrong.
var ErrDayOpen = errors.New("day is not closed yet")

// ErrStagedMismatch means staging does not hold what was inserted, so the day was not swapped in.
var ErrStagedMismatch = errors.New("staged rows do not match the built rows")

// Builder builds one day of mcp_connection_outcomes_daily from raw events and audit logs.
type Builder struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	ch     driver.Conn

	// backfillStart is the first day DaysToBuild considers.
	backfillStart time.Time
}

func NewBuilder(logger *slog.Logger, db *pgxpool.Pool, ch driver.Conn) *Builder {
	return &Builder{logger: logger.With(attr.SlogComponent("mcp_connection_outcomes_daily")), db: db, ch: ch, backfillStart: BackfillStart}
}

// Result is what building a day did.
type Result struct {
	// Rows is how many rows the day holds now, or would have held when not swapped.
	Rows int

	// Attempts is how many attempts those rows count.
	Attempts uint64

	// Swapped is false when the day had no attempts, which leaves the stored day as it was.
	Swapped bool
}

// StartOfDay is midnight UTC on t's UTC day.
func StartOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// closedAt is when a day's attempts have all ended.
func closedAt(day time.Time) time.Time {
	return day.Add(day24h + DayCloseDelay)
}

// rebuildUntil is when a day stops being rebuilt: one run after the last failure's
// recovery window ended.
func rebuildUntil(day time.Time) time.Time {
	return closedAt(day).Add(mcpconnectionattempts.RecoveryWindow + scheduleInterval)
}

// DaysToBuild lists the days a run at now should build, oldest first: closed days whose
// recovery window is still open, and older days with raw events that the long-term
// table lacks, which is how a first deploy backfills.
func (b *Builder) DaysToBuild(ctx context.Context, now time.Time) ([]time.Time, error) {
	today := StartOfDay(now)
	first := today.AddDate(0, 0, -rawSafeDays)
	if first.Before(b.backfillStart) {
		first = b.backfillStart
	}
	last := StartOfDay(now.Add(-day24h - DayCloseDelay))
	if last.Before(first) {
		return nil, nil
	}

	rawDays, err := b.distinctDays(ctx,
		`SELECT DISTINCT toDate(attempt_started_at) AS d FROM mcp_connection_events
		WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
		AND NOT has(?, organization_id)`,
		first.UnixNano(), last.Add(day24h).UnixNano(), excludedOrganizations())
	if err != nil {
		return nil, fmt.Errorf("list days with raw events: %w", err)
	}
	builtDays, err := b.distinctDays(ctx,
		`SELECT DISTINCT day AS d FROM `+mainTable+` WHERE day >= toDate(?) AND day <= toDate(?)`,
		first.Format(time.DateOnly), last.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("list built days: %w", err)
	}

	var days []time.Time
	for day := range rawDays {
		_, built := builtDays[day]
		if !built || now.Before(rebuildUntil(day)) {
			days = append(days, day)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	return days, nil
}

func (b *Builder) distinctDays(ctx context.Context, query string, args ...any) (map[time.Time]struct{}, error) {
	rows, err := b.ch.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query days: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	days := map[time.Time]struct{}{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan day: %w", err)
		}
		days[StartOfDay(d)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read days: %w", err)
	}
	return days, nil
}

// excludedOrganizations are left out of the rollup: the demo organization's attempts are made up.
func excludedOrganizations() []string {
	return []string{constants.DemoOrganizationID}
}

// BuildDay replaces one day of the long-term table. It is safe to rerun and to
// interrupt: the stored day changes only through one REPLACE PARTITION.
func (b *Builder) BuildDay(ctx context.Context, day, now time.Time) (Result, error) {
	day = StartOfDay(day)
	if now.Before(closedAt(day)) {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, fmt.Errorf("build %s: %w", day.Format(time.DateOnly), ErrDayOpen)
	}

	rows, err := b.outcomes(ctx, day, now)
	if err != nil {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, err
	}
	return b.publish(ctx, day, rows, totalsOf(rows))
}

// outcomes works out the day's rows: how each attempt ended and what fixed it.
func (b *Builder) outcomes(ctx context.Context, day, now time.Time) ([]Row, error) {
	counts := tally{}
	var failures []mcpconnectionattempts.AttemptSummary
	err := b.scanAttempts(ctx, attemptsQuery, func(a mcpconnectionattempts.AttemptSummary) {
		e := a.End(now)
		if e.NeedsFix() && a.SubjectURN != "" {
			failures = append(failures, a)
			return
		}
		counts.add(a, e, mcpconnectionattempts.FixedByNothing)
	}, day.UnixNano(), day.Add(day24h).UnixNano(), excludedOrganizations())
	if err != nil {
		return nil, fmt.Errorf("read attempts: %w", err)
	}
	if len(failures) == 0 {
		return counts.rows(), nil
	}

	var successes []mcpconnectionattempts.AttemptSummary
	err = b.scanAttempts(ctx, successesQuery, func(a mcpconnectionattempts.AttemptSummary) { successes = append(successes, a) },
		day.UnixNano(), day.Add(day24h).UnixNano(), day.Add(day24h+mcpconnectionattempts.RecoveryWindow).UnixNano(), excludedOrganizations())
	if err != nil {
		return nil, fmt.Errorf("read successes: %w", err)
	}
	index := newSuccessIndex(successes)

	type pair struct {
		failure, success mcpconnectionattempts.AttemptSummary
	}
	byOrg := map[string][]pair{}
	for _, f := range failures {
		s, ok := mcpconnectionattempts.NextSuccess(f, index[keyOf(f)], now)
		if !ok {
			counts.add(f, f.End(now), mcpconnectionattempts.FixedByNothing)
			continue
		}
		byOrg[f.OrganizationID] = append(byOrg[f.OrganizationID], pair{failure: f, success: s})
	}

	for org, pairs := range byOrg {
		subjects := map[string]struct{}{}
		after, before := pairs[0].failure.StartedAt, pairs[0].success.StartedAt
		for _, p := range pairs {
			for _, id := range mcpconnectionattempts.FixTargets(p.failure, p.success) {
				subjects[id] = struct{}{}
			}
			after = minTime(after, p.failure.StartedAt)
			before = maxTime(before, p.success.StartedAt)
		}
		changes, err := b.changes(ctx, org, subjects, after, before)
		if err != nil {
			return nil, err
		}
		for _, p := range pairs {
			counts.add(p.failure, p.failure.End(now), mcpconnectionattempts.FixedByOf(p.failure, p.success, changes))
		}
	}
	return counts.rows(), nil
}

// changes reads the audit entries of one organization on the given subjects.
func (b *Builder) changes(ctx context.Context, organizationID string, subjects map[string]struct{}, after, before time.Time) ([]mcpconnectionattempts.Change, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(subjects))
	for id := range subjects {
		ids = append(ids, id)
	}
	entries, err := auditrepo.New(b.db).ListAuditActorsOnSubjects(ctx, auditrepo.ListAuditActorsOnSubjectsParams{
		OrganizationID: organizationID,
		SubjectIds:     ids,
		After:          conv.ToPGTimestamptz(after),
		Before:         conv.ToPGTimestamptz(before),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	changes := make([]mcpconnectionattempts.Change, 0, len(entries))
	for _, entry := range entries {
		changes = append(changes, mcpconnectionattempts.Change{
			SubjectID: entry.SubjectID,
			ByPerson:  mcpconnectionattempts.IsPersonActor(entry.ActorType, entry.ActorID),
			At:        entry.CreatedAt.Time,
		})
	}
	return changes, nil
}

func (b *Builder) scanAttempts(ctx context.Context, query string, each func(mcpconnectionattempts.AttemptSummary), args ...any) error {
	rows, err := b.ch.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query attempts: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	for rows.Next() {
		var a mcpconnectionattempts.AttemptSummary
		if err := rows.ScanStruct(&a); err != nil {
			return fmt.Errorf("scan attempt: %w", err)
		}
		each(a)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read attempt rows: %w", err)
	}
	return nil
}

// attemptColumns reduces an attempt's rows to one, as mcpconnectionattempts.Summarize
// does in Go. The first final row decides, so a row written after it changes nothing.
// The client is the AI app when any row names one, since sign-in pages open in a browser.
const attemptColumns = `
	organization_id,
	attempt_id,
	min(attempt_started_at) AS started_at,
	toBool(countIf(is_final) > 0) AS has_final,
	toString(argMinIf(step, event_time, is_final)) AS final_step,
	toString(argMinIf(outcome, event_time, is_final)) AS final_outcome,
	toString(argMinIf(reason, event_time, is_final)) AS final_reason,
	toString(argMax(step, event_time)) AS last_step,
	toString(argMax(reason, event_time)) AS last_reason,
	anyIf(subject_urn, subject_urn != '') AS subject,
	any(app_key) AS app,
	any(server_id) AS server,
	anyIf(provider_host, provider_host != '') AS provider,
	toString(if(countIf(client_family NOT IN ('', 'browser')) > 0,
		anyIf(client_family, client_family NOT IN ('', 'browser')),
		anyIf(client_family, client_family != ''))) AS client,
	toUInt64(max(repeats)) + 1 AS weight,
	toBool(countIf(step_result = 'signed_in') > 0) AS signed_in,
	arraySort(arrayFilter(x -> x != '', arrayDistinct(arrayConcat(
		groupUniqArray(server_id), groupUniqArray(member_id), groupUniqArray(auth_client_id),
		groupUniqArray(remote_session_issuer_id), groupUniqArray(user_session_issuer_id),
		groupUniqArray(remote_session_client_id))))) AS touched`

// attemptsQuery reads every attempt that started on the day.
const attemptsQuery = `SELECT` + attemptColumns + `
FROM mcp_connection_events
WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
	AND NOT has(?, organization_id)
GROUP BY organization_id, attempt_id`

// successesQuery reads connected attempts that started from the day through the end
// of the recovery window, for people with a failure that day. Its arguments are the
// day's start and end, the window's end and the excluded organizations.
const successesQuery = `WITH failing AS (
	SELECT DISTINCT organization_id, subject_urn FROM mcp_connection_events
	WHERE attempt_started_at >= fromUnixTimestamp64Nano($1) AND attempt_started_at < fromUnixTimestamp64Nano($2)
		AND subject_urn != ''
		AND attempt_id IN (
			SELECT attempt_id FROM mcp_connection_events
			WHERE attempt_started_at >= fromUnixTimestamp64Nano($1) AND attempt_started_at < fromUnixTimestamp64Nano($2)
				AND reason != ''
		)
)
SELECT` + attemptColumns + `
FROM mcp_connection_events
WHERE attempt_started_at >= fromUnixTimestamp64Nano($1) AND attempt_started_at < fromUnixTimestamp64Nano($3)
	AND NOT has($4, organization_id)
	AND attempt_id IN (
		SELECT attempt_id FROM mcp_connection_events
		WHERE attempt_started_at >= fromUnixTimestamp64Nano($1) AND attempt_started_at < fromUnixTimestamp64Nano($3)
			AND (organization_id, subject_urn) IN (SELECT organization_id, subject_urn FROM failing)
	)
GROUP BY organization_id, attempt_id
HAVING argMinIf(outcome, event_time, is_final) = 'connected'`

// stagedTotals is what staging must hold for a day before it is swapped in.
type stagedTotals struct {
	rows     uint64
	attempts uint64
}

func totalsOf(rows []Row) stagedTotals {
	totals := stagedTotals{rows: uint64(len(rows)), attempts: 0}
	for _, row := range rows {
		totals.attempts += row.Attempts
	}
	return totals
}

// publish swaps a day in: clear its staging partition, insert, check staging holds
// exactly what was inserted, replace the stored day, then clear staging. A day with no
// rows is left as stored, since REPLACE PARTITION from an empty partition would wipe it.
// Correctness does not rest on insert deduplication: it is off, and staging is cleared first.
func (b *Builder) publish(ctx context.Context, day time.Time, rows []Row, want stagedTotals) (Result, error) {
	partition := day.Format("20060102")
	result := Result{Rows: len(rows), Attempts: want.attempts, Swapped: false}

	if err := b.dropStaging(ctx, partition); err != nil {
		return result, err
	}
	if len(rows) == 0 {
		b.logger.InfoContext(ctx, "no connection attempts to roll up, day left as stored", attr.SlogValueString(day.Format(time.DateOnly)))
		return result, nil
	}

	insertCtx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":       0,
		"insert_deduplicate": 0,
	}))
	batch, err := b.ch.PrepareBatch(insertCtx, `INSERT INTO `+stagingTable+` (day, provider, app, client_family, step, outcome, reason, fixed_by, attempts)`)
	if err != nil {
		return result, fmt.Errorf("prepare staging insert: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return batch.Abort() })
	for _, row := range rows {
		if err := batch.Append(day, row.Provider, row.App, row.ClientFamily, row.Step, row.Outcome, row.Reason, string(row.FixedBy), row.Attempts); err != nil {
			return result, fmt.Errorf("append staging row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return result, fmt.Errorf("insert staging rows: %w", err)
	}

	var staged stagedTotals
	if err := b.ch.QueryRow(ctx, `SELECT count(), sum(attempts) FROM `+stagingTable+` WHERE day = toDate(?)`, day.Format(time.DateOnly)).
		Scan(&staged.rows, &staged.attempts); err != nil {
		return result, fmt.Errorf("count staged rows: %w", err)
	}
	if staged != want {
		return result, fmt.Errorf("%w: staged %d rows and %d attempts, built %d rows and %d attempts",
			ErrStagedMismatch, staged.rows, staged.attempts, want.rows, want.attempts)
	}

	if err := b.ch.Exec(ctx, `ALTER TABLE `+mainTable+` REPLACE PARTITION ID '`+partition+`' FROM `+stagingTable); err != nil {
		return result, fmt.Errorf("replace day partition: %w", err)
	}
	result.Swapped = true

	if err := b.dropStaging(ctx, partition); err != nil {
		return result, err
	}
	return result, nil
}

func (b *Builder) dropStaging(ctx context.Context, partition string) error {
	if err := b.ch.Exec(ctx, `ALTER TABLE `+stagingTable+` DROP PARTITION ID '`+partition+`'`); err != nil {
		return fmt.Errorf("clear staging partition: %w", err)
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
