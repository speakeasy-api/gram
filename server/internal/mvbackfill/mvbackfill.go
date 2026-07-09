// Package mvbackfill rebuilds tenant-scoped slices of a ClickHouse
// materialized-view target from its raw source table using the Null-table
// staging pattern.
//
// The pattern needs four tables wired up in the schema (see the
// attribute_metrics_summaries backfill DDL in server/clickhouse/schema.sql
// for the reference layout):
//
//   - a Null-engine clone of the source table (the feed),
//   - a staging table with the live target's exact schema,
//   - a materialized view from the feed into staging repeating the live MV's
//     SELECT (minus any ingest-time cutoff), so the transform is always the
//     deployed MV SQL — never a copy that can drift,
//   - an archive table with the live schema plus a leading backfill_run_id
//     column, for pre-delete snapshots.
//
// The Driver runs the per-tenant lifecycle against those tables:
//
//  1. Prepare    — clear the tenant's staging leftovers, measure the raw
//     source window below the boundary.
//  2. StageChunk — replay one time slice of raw rows through the feed; the
//     staging MV rebuilds the aggregates into the staging table.
//  3. Archive    — snapshot the live rows the commit will delete.
//  4. Commit     — delete the live window, insert the staged rows.
//  5. Cleanup    — clear the tenant's staging rows.
//
// Validation between staging and live sits between steps 2 and 3 and is left
// to the caller: the invariants worth checking are specific to each MV's
// aggregate columns.
//
// All steps are idempotent so callers (Temporal activities) can retry safely:
// Prepare and Cleanup are deletes, StageChunk re-runs are absorbed by
// Prepare's staging clear on a fresh run, Archive clears its run's rows
// before re-inserting, and Commit's synchronous delete also clears any
// partial insert from a failed prior attempt.
package mvbackfill

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Spec wires one materialized-view backfill into the driver. All identifiers
// are interpolated into SQL, so they must be compile-time constants — only
// tenant IDs and time bounds are bound as query parameters.
type Spec struct {
	// SourceTable is the raw table the rebuild replays (e.g. telemetry_logs).
	SourceTable string

	// SourceTimeColumn is the Int64 unix-nano event-time column on
	// SourceTable that windows the replay.
	SourceTimeColumn string

	// SourceColumns is the explicit list of SourceTable's ordinary
	// (non-materialized) columns, shared with FeedTable. A positional
	// SELECT * would break when the physical column order of the migrated
	// source table differs from the freshly created feed table (columns
	// added via ALTER land at the end of the source but in declared order
	// on the feed).
	SourceColumns string

	// FeedTable is the Null-engine clone of SourceTable that the staging MV
	// reads from.
	FeedTable string

	// LiveTable is the materialized view's target table being rebuilt.
	LiveTable string

	// StagingTable receives the rebuilt rows from the staging MV. Identical
	// schema to LiveTable.
	StagingTable string

	// ArchiveTable receives pre-delete snapshots of live rows. Schema is
	// LiveTable's columns prefixed with a backfill_run_id column.
	ArchiveTable string

	// TenantColumn scopes every statement to one tenant
	// (e.g. gram_project_id).
	TenantColumn string

	// BucketColumn is the DateTime aggregation bucket column on the live,
	// staging, and archive tables.
	BucketColumn string

	// Columns is the explicit shared column list of the live, staging, and
	// archive tables, keeping the INSERT ... SELECTs immune to column order
	// drift.
	Columns string
}

// Driver executes the staging lifecycle for a single Spec. It is stateless
// beyond the connection and safe for concurrent use across tenants.
type Driver struct {
	conn clickhouse.Conn
	spec Spec
}

func NewDriver(conn clickhouse.Conn, spec Spec) *Driver {
	return &Driver{conn: conn, spec: spec}
}

// mutationCtx makes ALTER ... DELETE block until the mutation has been
// applied on the server, so a returned nil error means the rows are actually
// gone and a follow-up INSERT cannot race the delete.
func mutationCtx(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"mutations_sync": 1,
	}))
}

// SourceWindow measures the tenant's raw source rows below the boundary.
// RowCount zero means there is nothing to backfill (MinTime/MaxTime are the
// unix epoch in that case).
type SourceWindow struct {
	RowCount uint64
	MinTime  time.Time
	MaxTime  time.Time
}

// Prepare clears any staging leftovers for the tenant (from an earlier
// aborted or crashed run) and measures the tenant's raw source window below
// the boundary. The window bounds drive the caller's chunked staging loop.
func (d *Driver) Prepare(ctx context.Context, tenant string, boundary time.Time) (*SourceWindow, error) {
	if err := d.conn.Exec(mutationCtx(ctx),
		"ALTER TABLE "+d.spec.StagingTable+" DELETE WHERE "+d.spec.TenantColumn+" = ?",
		tenant,
	); err != nil {
		return nil, fmt.Errorf("clear staging rows: %w", err)
	}

	var count uint64
	var minNano, maxNano int64
	if err := d.conn.QueryRow(ctx,
		"SELECT count(), coalesce(min("+d.spec.SourceTimeColumn+"), 0), coalesce(max("+d.spec.SourceTimeColumn+"), 0) "+
			"FROM "+d.spec.SourceTable+" WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.SourceTimeColumn+" < ?",
		tenant, boundary.UnixNano(),
	).Scan(&count, &minNano, &maxNano); err != nil {
		return nil, fmt.Errorf("measure source window: %w", err)
	}

	return &SourceWindow{
		RowCount: count,
		MinTime:  time.Unix(0, minNano).UTC(),
		MaxTime:  time.Unix(0, maxNano).UTC(),
	}, nil
}

// StageChunk replays the tenant's raw source rows in [from, to) through the
// Null-engine feed. The attached staging MV performs the actual rollup into
// the staging table. Only the ordinary columns are carried by name (never
// positionally — the migrated source table's physical order differs from the
// feed's declared order); the feed table recomputes the materialized columns
// the MV reads.
func (d *Driver) StageChunk(ctx context.Context, tenant string, from time.Time, to time.Time) error {
	if err := d.conn.Exec(ctx,
		"INSERT INTO "+d.spec.FeedTable+" ("+d.spec.SourceColumns+") "+
			"SELECT "+d.spec.SourceColumns+" FROM "+d.spec.SourceTable+
			" WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.SourceTimeColumn+" >= ? AND "+d.spec.SourceTimeColumn+" < ?",
		tenant, from.UnixNano(), to.UnixNano(),
	); err != nil {
		return fmt.Errorf("stage source chunk: %w", err)
	}
	return nil
}

// ArchiveResult reports the pre-commit snapshot. DeleteWindowStart is
// min(bucket) of the staged rebuild — the commit deletes live rows in
// [DeleteWindowStart, boundary). Live buckets older than the staged coverage
// are never touched (the raw-retention clamp: summaries whose raw rows
// already expired cannot be rebuilt, so they are not deleted).
type ArchiveResult struct {
	ArchivedRowCount  uint64
	DeleteWindowStart time.Time
}

// Archive snapshots the tenant's live rows in the delete window into the
// archive table, keyed by runID for post-commit restores. It must fully
// succeed before Commit runs so the snapshot is taken from untouched live
// data; re-runs first clear this run's rows, making it idempotent.
func (d *Driver) Archive(ctx context.Context, tenant string, boundary time.Time, runID string) (*ArchiveResult, error) {
	if runID == "" {
		return nil, fmt.Errorf("backfill run ID is required")
	}

	windowStart, err := d.stagingWindowStart(ctx, tenant)
	if err != nil {
		return nil, err
	}

	if err := d.conn.Exec(mutationCtx(ctx),
		"ALTER TABLE "+d.spec.ArchiveTable+" DELETE WHERE backfill_run_id = ?",
		runID,
	); err != nil {
		return nil, fmt.Errorf("clear prior archive attempt: %w", err)
	}

	if err := d.conn.Exec(ctx,
		"INSERT INTO "+d.spec.ArchiveTable+" (backfill_run_id, "+d.spec.Columns+") "+
			"SELECT ?, "+d.spec.Columns+" FROM "+d.spec.LiveTable+
			" WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.BucketColumn+" >= ? AND "+d.spec.BucketColumn+" < ?",
		runID, tenant, windowStart, boundary,
	); err != nil {
		return nil, fmt.Errorf("archive live rows: %w", err)
	}

	var archived uint64
	if err := d.conn.QueryRow(ctx,
		"SELECT count() FROM "+d.spec.ArchiveTable+" WHERE backfill_run_id = ?",
		runID,
	).Scan(&archived); err != nil {
		return nil, fmt.Errorf("count archived rows: %w", err)
	}

	return &ArchiveResult{
		ArchivedRowCount:  archived,
		DeleteWindowStart: windowStart,
	}, nil
}

// CommitResult reports the swap. InsertedRowCount is the tenant's live row
// count inside the swapped window right after the insert (pre-merge, so it
// roughly matches the staging count).
type CommitResult struct {
	DeleteWindowStart time.Time
	InsertedRowCount  uint64
}

// Commit swaps the staged rebuild into the live table: delete the tenant's
// live rows in [staging min bucket, boundary), then insert the staged rows.
// The synchronous delete also clears any partial insert left by a failed
// earlier attempt, so retrying the whole operation is safe. Buckets at or
// after the boundary were written by the live MV during the run and are
// untouched — no gap, no double count.
func (d *Driver) Commit(ctx context.Context, tenant string, boundary time.Time) (*CommitResult, error) {
	windowStart, err := d.stagingWindowStart(ctx, tenant)
	if err != nil {
		return nil, err
	}

	if err := d.conn.Exec(mutationCtx(ctx),
		"ALTER TABLE "+d.spec.LiveTable+" DELETE WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.BucketColumn+" >= ? AND "+d.spec.BucketColumn+" < ?",
		tenant, windowStart, boundary,
	); err != nil {
		return nil, fmt.Errorf("delete live window: %w", err)
	}

	if err := d.conn.Exec(ctx,
		"INSERT INTO "+d.spec.LiveTable+" ("+d.spec.Columns+") "+
			"SELECT "+d.spec.Columns+" FROM "+d.spec.StagingTable+
			" WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.BucketColumn+" < ?",
		tenant, boundary,
	); err != nil {
		return nil, fmt.Errorf("insert staged rows into live table: %w", err)
	}

	var inserted uint64
	if err := d.conn.QueryRow(ctx,
		"SELECT count() FROM "+d.spec.LiveTable+
			" WHERE "+d.spec.TenantColumn+" = ? AND "+d.spec.BucketColumn+" >= ? AND "+d.spec.BucketColumn+" < ?",
		tenant, windowStart, boundary,
	).Scan(&inserted); err != nil {
		return nil, fmt.Errorf("count swapped live rows: %w", err)
	}

	return &CommitResult{
		DeleteWindowStart: windowStart,
		InsertedRowCount:  inserted,
	}, nil
}

// stagingWindowStart returns min(bucket) of the tenant's staged rows — the
// lower bound of the live delete window. Erroring on an empty staging table
// protects against ever deleting live rows that nothing would replace.
func (d *Driver) stagingWindowStart(ctx context.Context, tenant string) (time.Time, error) {
	var count uint64
	var minBucket time.Time
	if err := d.conn.QueryRow(ctx,
		"SELECT count(), coalesce(min("+d.spec.BucketColumn+"), toDateTime(0, 'UTC')) FROM "+d.spec.StagingTable+
			" WHERE "+d.spec.TenantColumn+" = ?",
		tenant,
	).Scan(&count, &minBucket); err != nil {
		return time.Time{}, fmt.Errorf("query staging window: %w", err)
	}
	if count == 0 {
		return time.Time{}, fmt.Errorf("staging table has no rows for tenant %s", tenant)
	}
	return minBucket.UTC(), nil
}

// Cleanup clears the tenant's staged rows. Safe to run after a commit and
// after an abort; a failure here never invalidates a committed swap (Prepare
// re-clears on the next run anyway).
func (d *Driver) Cleanup(ctx context.Context, tenant string) error {
	if err := d.conn.Exec(mutationCtx(ctx),
		"ALTER TABLE "+d.spec.StagingTable+" DELETE WHERE "+d.spec.TenantColumn+" = ?",
		tenant,
	); err != nil {
		return fmt.Errorf("clear staging rows: %w", err)
	}
	return nil
}
