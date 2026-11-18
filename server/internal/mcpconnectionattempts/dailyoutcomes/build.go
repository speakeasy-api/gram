package dailyoutcomes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// DayCloseDelay covers the live window of an attempt started just before midnight, plus late rows.
	DayCloseDelay = 90 * time.Minute

	// rawRetentionDays is the raw events table's TTL.
	rawRetentionDays = 30

	// RawSafeDays is the oldest day built: day partitions drop whole parts, so D is
	// complete until D+30. A plan lists at most RawSafeDays+1 days.
	RawSafeDays = 29

	// scheduleInterval is how often the job runs.
	scheduleInterval = 24 * time.Hour

	// rebuildGrace rebuilds a day for three runs after its lookahead, surviving a two-day outage.
	rebuildGrace = 3 * scheduleInterval

	// day24h is one UTC day.
	day24h = 24 * time.Hour

	// ReadTimeout lifts the connection's 60-second limit for one read over nine days of
	// events. A build makes ReadsPerBuild such reads, so its activity timeout covers them all.
	ReadTimeout = 10 * time.Minute

	// ReadsPerBuild is how many ReadTimeout reads one BuildDay or DaysToBuild can make.
	ReadsPerBuild = 2

	// readMemoryBytes caps one read. Up to four days build at once.
	readMemoryBytes = 2 << 30 // 2 GiB

	stagingTable = "mcp_connection_outcomes_daily_staging"
)

// BackfillStart is the first whole day of raw connection events. They began on the
// afternoon of 2026-11-04, and a partial day would read as a false dip.
var BackfillStart = time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)

// ErrDayOpen means the day can still gain attempts, so building it now would be wrong.
var ErrDayOpen = errors.New("day is not closed yet")

// ErrDayExpiring means the day's raw events may already be partly gone, so it is not rebuilt.
var ErrDayExpiring = errors.New("day is too old to rebuild from raw events")

// ErrStagedMismatch means staging does not hold what was inserted, so the day was not swapped in.
var ErrStagedMismatch = errors.New("staged rows do not match the built rows")

// ErrStoredMismatch means the stored day does not hold what was built after the swap,
// such as when another build of the day swapped in between. A rerun fixes it.
var ErrStoredMismatch = errors.New("stored day does not match the built rows")

// Builder builds one day of mcp_connection_outcomes_daily from raw events and audit logs.
type Builder struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	ch     driver.Conn

	// skippedDays counts unbuilt days whose raw events are too old to build.
	skippedDays metric.Int64Counter

	// unknownLabels counts values outside the known steps, outcomes, reasons and fixes.
	unknownLabels metric.Int64Counter

	// backfillStart is the first day DaysToBuild considers.
	backfillStart time.Time

	// clock is the wall clock, which decides whether raw events are still whole.
	clock func() time.Time
}

func NewBuilder(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, ch driver.Conn) *Builder {
	logger = logger.With(attr.SlogComponent("mcp_connection_outcomes_daily"))
	meter := meterProvider.Meter(meterScope)
	skipped, err := meter.Int64Counter(meterSkippedDays,
		metric.WithDescription("Days with raw connection events that were never built and are now too old to build."),
		metric.WithUnit("{day}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterSkippedDays), attr.SlogError(err))
	}
	unknown, err := meter.Int64Counter(meterUnknownLabels,
		metric.WithDescription("Attempt values outside the known steps, outcomes, reasons and fixes, stored as other."),
		metric.WithUnit("{attempt}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterUnknownLabels), attr.SlogError(err))
	}
	return &Builder{logger: logger, db: db, ch: ch, skippedDays: skipped, unknownLabels: unknown, backfillStart: BackfillStart, clock: time.Now}
}

const (
	meterScope       = "github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/dailyoutcomes"
	meterSkippedDays = "mcp.connection.outcomes.skipped_days"

	meterUnknownLabels = "mcp.connection.outcomes.unknown_labels"
)

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

// rebuildUntil is when a day stops being rebuilt: rebuildGrace after the last
// failure's fix lookahead ended.
func rebuildUntil(day time.Time) time.Time {
	return closedAt(day).Add(mcpconnectionattempts.FixLookahead + rebuildGrace)
}

// readContext lifts the connection's query time limit and caps memory for the job's reads.
func readContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": int(ReadTimeout / time.Second),
		"max_memory_usage":   readMemoryBytes,
	}))
}

// consistentContext makes a read see every write acknowledged before it, on any replica.
func consistentContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"select_sequential_consistency": 1,
	}))
}

// DaysToBuild lists, oldest first, closed days still being rebuilt and unbuilt days with raw events.
func (b *Builder) DaysToBuild(ctx context.Context, now time.Time) ([]time.Time, error) {
	today := StartOfDay(now)
	scanFrom := maxTime(b.backfillStart, today.AddDate(0, 0, -rawRetentionDays))
	first := maxTime(b.backfillStart, today.AddDate(0, 0, -RawSafeDays))
	last := StartOfDay(now.Add(-day24h - DayCloseDelay))
	if last.Before(scanFrom) {
		return nil, nil
	}

	rawDays, err := b.distinctDays(ctx,
		`SELECT DISTINCT toDate(attempt_started_at) AS d FROM mcp_connection_events
		WHERE attempt_started_at >= fromUnixTimestamp64Nano(?) AND attempt_started_at < fromUnixTimestamp64Nano(?)
		AND NOT has(?, organization_id)`,
		scanFrom.UnixNano(), last.Add(day24h).UnixNano(), mcpconnectionattempts.ExcludedOrganizations())
	if err != nil {
		return nil, fmt.Errorf("list days with raw events: %w", err)
	}
	builtDays, err := b.distinctDays(ctx,
		`SELECT DISTINCT day AS d FROM `+mcpconnectionattempts.OutcomesDailyTable+` WHERE day >= toDate(?) AND day <= toDate(?)`,
		scanFrom.Format(time.DateOnly), last.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("list built days: %w", err)
	}

	var days []time.Time
	for day := range rawDays {
		_, built := builtDays[day]
		switch {
		case day.Before(first) && !built:
			b.logger.ErrorContext(ctx, "connection outcomes day was never built and its raw events are expiring", attr.SlogValueString(day.Format(time.DateOnly)))
			b.skippedDays.Add(ctx, 1)
		case day.Before(first):
		case !built || now.Before(rebuildUntil(day)):
			days = append(days, day)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	return days, nil
}

func (b *Builder) distinctDays(ctx context.Context, query string, args ...any) (map[time.Time]struct{}, error) {
	rows, err := b.ch.Query(readContext(ctx), query, args...)
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

// BuildDay replaces one day of the long-term table. It is safe to rerun and to
// interrupt: the stored day changes only through one REPLACE PARTITION.
func (b *Builder) BuildDay(ctx context.Context, day, now time.Time) (Result, error) {
	day = StartOfDay(day)
	if now.Before(closedAt(day)) {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, fmt.Errorf("build %s: %w", day.Format(time.DateOnly), ErrDayOpen)
	}

	if err := b.checkRawWhole(day); err != nil {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, err
	}
	rows, err := b.outcomes(ctx, day, now)
	if err != nil {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, err
	}
	// The read may have crossed midnight, so check again before replacing the stored day.
	if err := b.checkRawWhole(day); err != nil {
		return Result{Rows: 0, Attempts: 0, Swapped: false}, err
	}
	result, err := b.publish(ctx, day, rows, totalsOf(rows))
	if errors.Is(err, ErrStoredMismatch) {
		// The swap went in but the stored day is wrong: a replica that had not yet seen
		// the staging insert, or an overlapping build of the same day. Publish once more
		// so a wrong day is not left in place. A day still wrong after every attempt
		// stays wrong until a later run rebuilds it, or for good once its rebuild window ends.
		b.logger.WarnContext(ctx, "stored connection outcomes day did not match its build, publishing again", attr.SlogValueString(day.Format(time.DateOnly)), attr.SlogError(err))
		swapped := result.Swapped
		result, err = b.publish(ctx, day, rows, totalsOf(rows))
		result.Swapped = result.Swapped || swapped
	}
	return result, err
}

// checkRawWhole refuses a day whose raw events may already be partly expired by the wall clock.
func (b *Builder) checkRawWhole(day time.Time) error {
	if day.Before(StartOfDay(b.clock()).AddDate(0, 0, -RawSafeDays)) {
		return fmt.Errorf("build %s: %w", day.Format(time.DateOnly), ErrDayExpiring)
	}
	return nil
}

// outcomes works out the day's rows: how each attempt ended and what fixed it.
func (b *Builder) outcomes(ctx context.Context, day, now time.Time) ([]DayRow, error) {
	counts := newTally()
	var failures []mcpconnectionattempts.AttemptSummary
	dayFilter := mcpconnectionattempts.AttemptFilter{
		StartedFrom: day, StartedBefore: day.Add(day24h), OrganizationID: "", ExcludeOrganizationIDs: mcpconnectionattempts.ExcludedOrganizations(),
		ProjectID: uuid.Nil, ServerID: "", PeopleFailingFrom: time.Time{}, PeopleFailingBefore: time.Time{}, FixCandidatesOnly: false,
	}
	err := mcpconnectionattempts.ReadAttempts(readContext(ctx), b.ch, dayFilter, func(a mcpconnectionattempts.AttemptSummary) {
		e := a.End(now)
		// Until AIM-1027, a refused used token stays unpaired, as when it named nobody.
		if e.NeedsFix() && a.SubjectURN != "" && e.Reason != mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed.String() {
			failures = append(failures, a)
			return
		}
		counts.add(a, e, mcpconnectionattempts.FixedByNothing)
	})
	if err != nil {
		return nil, fmt.Errorf("read attempts: %w", err)
	}

	if len(failures) > 0 {
		var candidates []mcpconnectionattempts.AttemptSummary
		successFilter := dayFilter
		successFilter.StartedBefore = day.Add(day24h + mcpconnectionattempts.FixLookahead)
		successFilter.PeopleFailingFrom, successFilter.PeopleFailingBefore = day, day.Add(day24h)
		successFilter.FixCandidatesOnly = true
		err = mcpconnectionattempts.ReadAttempts(readContext(ctx), b.ch, successFilter, func(a mcpconnectionattempts.AttemptSummary) { candidates = append(candidates, a) })
		if err != nil {
			return nil, fmt.Errorf("read fix candidates: %w", err)
		}
		if err := b.addFixes(ctx, counts, failures, mcpconnectionattempts.IndexSuccesses(candidates, now)); err != nil {
			return nil, err
		}
	}

	for column, n := range counts.unknown {
		b.logger.ErrorContext(ctx, "connection attempts with an unknown value were stored as other", attr.SlogName(column), attr.SlogValueInt(n))
		b.unknownLabels.Add(ctx, int64(n))
	}
	return counts.rows(), nil
}

// addFixes pairs each failure with its success and names the fix, reading audit
// changes once per organization.
func (b *Builder) addFixes(ctx context.Context, counts *tally, failures []mcpconnectionattempts.AttemptSummary, successes mcpconnectionattempts.Successes) error {
	byOrg := map[string][]mcpconnectionattempts.Fix{}
	for _, f := range failures {
		fix, ok := successes.Next(f)
		if !ok {
			counts.add(f, fix.Failed, mcpconnectionattempts.FixedByNothing)
			continue
		}
		byOrg[f.OrganizationID] = append(byOrg[f.OrganizationID], fix)
	}

	for org, fixes := range byOrg {
		subjects := map[string]struct{}{}
		after, before := fixes[0].Failed.At, fixes[0].Success.StartedAt
		for _, fix := range fixes {
			for _, id := range fix.Targets() {
				subjects[id] = struct{}{}
			}
			after, before = minTime(after, fix.Failed.At), maxTime(before, fix.Success.StartedAt)
		}
		changes, err := mcpconnectionattempts.ListChanges(ctx, b.db, org, slices.Collect(maps.Keys(subjects)), mcpconnectionattempts.AuditActions(fixes), after, before)
		if err != nil {
			return fmt.Errorf("read changes: %w", err)
		}
		for _, fix := range fixes {
			counts.add(fix.Failure, fix.Failed, fix.FixedBy(changes))
		}
	}
	return nil
}

// stagedTotals is what a table must hold for a day.
type stagedTotals struct {
	rows     uint64
	attempts uint64
}

func totalsOf(rows []DayRow) stagedTotals {
	totals := stagedTotals{rows: uint64(len(rows)), attempts: 0}
	for _, row := range rows {
		totals.attempts += row.Attempts
	}
	return totals
}

// publish clears staging, inserts, checks, swaps the day in, checks again and clears; an empty day is left as stored.
func (b *Builder) publish(ctx context.Context, day time.Time, rows []DayRow, want stagedTotals) (Result, error) {
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
		if err := batch.Append(day, row.Provider, row.App, row.ClientFamily, row.Step, row.Outcome, row.Reason, row.FixedBy, row.Attempts); err != nil {
			return result, fmt.Errorf("append staging row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return result, fmt.Errorf("insert staging rows: %w", err)
	}

	staged, err := b.totals(ctx, stagingTable, day)
	if err != nil {
		return result, err
	}
	if staged != want {
		return result, fmt.Errorf("%w: staged %d rows and %d attempts, built %d rows and %d attempts",
			ErrStagedMismatch, staged.rows, staged.attempts, want.rows, want.attempts)
	}

	if err := b.ch.Exec(ctx, `ALTER TABLE `+mcpconnectionattempts.OutcomesDailyTable+` REPLACE PARTITION ID '`+partition+`' FROM `+stagingTable); err != nil {
		return result, fmt.Errorf("replace day partition: %w", err)
	}
	result.Swapped = true

	stored, err := b.totals(ctx, mcpconnectionattempts.OutcomesDailyTable, day)
	if err != nil {
		return result, err
	}
	if stored != want {
		return result, fmt.Errorf("%w: stored %d rows and %d attempts, built %d rows and %d attempts",
			ErrStoredMismatch, stored.rows, stored.attempts, want.rows, want.attempts)
	}

	if err := b.dropStaging(ctx, partition); err != nil {
		return result, err
	}
	return result, nil
}

func (b *Builder) totals(ctx context.Context, table string, day time.Time) (stagedTotals, error) {
	var totals stagedTotals
	if err := b.ch.QueryRow(consistentContext(ctx), `SELECT count(), sum(attempts) FROM `+table+` WHERE day = toDate(?)`, day.Format(time.DateOnly)).
		Scan(&totals.rows, &totals.attempts); err != nil {
		return totals, fmt.Errorf("count rows in %s: %w", table, err)
	}
	return totals, nil
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
