package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mvbackfill"
)

// Tenant-scoped rebuild of attribute_metrics_summaries from raw
// telemetry_logs. The staging/commit mechanics live in the generic
// mvbackfill driver; this file holds only what is specific to this MV — the
// table wiring (Spec) and the validation stats over its aggregate columns.

// attributeMetricsSummaryColumns is the full column list shared by the live,
// staging, and archive tables (the archive prefixes backfill_run_id).
// Explicit lists keep the INSERT ... SELECTs immune to column order drift.
const attributeMetricsSummaryColumns = "gram_project_id, time_bucket, " +
	"department_name, job_title, employee_type, division_name, cost_center_name, user_email, " +
	"model, hook_source, roles, groups, " +
	"total_chats, total_input_tokens, total_output_tokens, total_tokens, " +
	"cache_read_input_tokens, cache_creation_input_tokens, total_cost, total_tool_calls, unique_tool_calls, " +
	"account_type, provider, billing_mode, " +
	"query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name"

// telemetryLogsSourceColumns is the ordinary (non-materialized) column list
// shared by telemetry_logs and telemetry_logs_backfill_feed. Keep in sync
// with both table definitions in server/clickhouse/schema.sql.
const telemetryLogsSourceColumns = "id, time_unix_nano, observed_time_unix_nano, observed_timestamp, " +
	"severity_text, body, trace_id, span_id, attributes, resource_attributes, " +
	"gram_project_id, gram_deployment_id, gram_function_id, gram_urn, " +
	"service_name, service_version, gram_chat_id"

// attributeMetricsBackfillSpec wires the attribute_metrics_summaries rebuild
// into the generic driver. The backfill DDL for the feed, staging, and
// archive tables lives in server/clickhouse/schema.sql.
var attributeMetricsBackfillSpec = mvbackfill.Spec{
	SourceTable:      "telemetry_logs",
	SourceTimeColumn: "time_unix_nano",
	SourceColumns:    telemetryLogsSourceColumns,
	FeedTable:        "telemetry_logs_backfill_feed",
	LiveTable:        "attribute_metrics_summaries",
	StagingTable:     "attribute_metrics_summaries_backfill",
	ArchiveTable:     "attribute_metrics_summaries_backfill_archive",
	TenantColumn:     "gram_project_id",
	BucketColumn:     "time_bucket",
	Columns:          attributeMetricsSummaryColumns,
}

type BackfillAttributeMetricsSummaries struct {
	logger *slog.Logger
	conn   clickhouse.Conn
	driver *mvbackfill.Driver
}

func NewBackfillAttributeMetricsSummaries(logger *slog.Logger, conn clickhouse.Conn) *BackfillAttributeMetricsSummaries {
	return &BackfillAttributeMetricsSummaries{
		logger: logger.With(attr.SlogComponent("backfill_attribute_metrics_summaries")),
		conn:   conn,
		driver: mvbackfill.NewDriver(conn, attributeMetricsBackfillSpec),
	}
}

type PrepareAttributeMetricsBackfillParams struct {
	ProjectID        string `json:"project_id"`
	BoundaryUnixNano int64  `json:"boundary_unix_nano"`
}

type PrepareAttributeMetricsBackfillResult struct {
	// RawRowCount is the number of raw telemetry_logs rows in the rebuild
	// window. Zero means there is nothing to backfill.
	RawRowCount     uint64 `json:"raw_row_count"`
	MinTimeUnixNano int64  `json:"min_time_unix_nano"`
	MaxTimeUnixNano int64  `json:"max_time_unix_nano"`
}

// Prepare clears any staging leftovers for the tenant (from an earlier
// aborted or crashed run) and measures the tenant's raw-log window below the
// boundary. The window bounds drive the workflow's day-chunked staging loop.
func (b *BackfillAttributeMetricsSummaries) Prepare(ctx context.Context, params PrepareAttributeMetricsBackfillParams) (*PrepareAttributeMetricsBackfillResult, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	stop := startActivityHeartbeat(ctx)
	defer stop()

	window, err := b.driver.Prepare(ctx, projectID.String(), time.Unix(0, params.BoundaryUnixNano).UTC())
	if err != nil {
		return nil, fmt.Errorf("prepare backfill: %w", err)
	}

	b.logger.InfoContext(ctx, "prepared attribute metrics backfill",
		attr.SlogProjectID(projectID.String()),
	)

	return &PrepareAttributeMetricsBackfillResult{
		RawRowCount:     window.RowCount,
		MinTimeUnixNano: window.MinTime.UnixNano(),
		MaxTimeUnixNano: window.MaxTime.UnixNano(),
	}, nil
}

type StageAttributeMetricsBackfillChunkParams struct {
	ProjectID    string `json:"project_id"`
	FromUnixNano int64  `json:"from_unix_nano"`
	ToUnixNano   int64  `json:"to_unix_nano"`
}

// StageChunk replays one time slice of the tenant's raw logs through the
// Null-engine feed; the attached backfill MV performs the rollup into the
// staging table.
func (b *BackfillAttributeMetricsSummaries) StageChunk(ctx context.Context, params StageAttributeMetricsBackfillChunkParams) error {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project ID: %w", err)
	}

	stop := startActivityHeartbeat(ctx)
	defer stop()

	if err := b.driver.StageChunk(ctx, projectID.String(),
		time.Unix(0, params.FromUnixNano).UTC(),
		time.Unix(0, params.ToUnixNano).UTC(),
	); err != nil {
		return fmt.Errorf("stage raw log chunk: %w", err)
	}
	return nil
}

type ValidateAttributeMetricsBackfillParams struct {
	ProjectID        string `json:"project_id"`
	BoundaryUnixNano int64  `json:"boundary_unix_nano"`
}

// AttributeMetricsBackfillTableStats summarizes one side (staging or live) of
// the pending swap so the operator can compare conserved invariants — totals
// only move where the MV logic intentionally changed.
type AttributeMetricsBackfillTableStats struct {
	RowCount                 uint64  `json:"row_count"`
	MinTimeBucketUnixSec     int64   `json:"min_time_bucket_unix_sec"`
	MaxTimeBucketUnixSec     int64   `json:"max_time_bucket_unix_sec"`
	TotalCost                float64 `json:"total_cost"`
	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	// TotalToolCalls counts tool rows (legacy column, kept warm during the
	// expand-contract transition); UniqueToolCalls dedups by call identity and
	// is what readers consume. Live rows written before the provenance-first
	// MV have empty unique_tool_calls states, so a staging-vs-live gap here is
	// expected on first rebuild.
	TotalToolCalls  uint64 `json:"total_tool_calls"`
	UniqueToolCalls uint64 `json:"unique_tool_calls"`
	TotalChats      uint64 `json:"total_chats"`
}

type ValidateAttributeMetricsBackfillResult struct {
	// Staging covers everything the backfill rebuilt for the tenant.
	Staging AttributeMetricsBackfillTableStats `json:"staging"`
	// Live covers the delete window [staging min bucket, boundary) — exactly
	// the live rows the commit step will replace.
	Live AttributeMetricsBackfillTableStats `json:"live"`
}

// Validate produces side-by-side stats of the staged rebuild and the live
// rows it would replace. This is deliberately MV-specific (it aggregates this
// MV's -State columns), so it lives here rather than in the driver. The
// workflow surfaces the result via a query handler and then blocks on the
// operator's approve/abort signal.
func (b *BackfillAttributeMetricsSummaries) Validate(ctx context.Context, params ValidateAttributeMetricsBackfillParams) (*ValidateAttributeMetricsBackfillResult, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	staging, err := b.tableStats(ctx, attributeMetricsBackfillSpec.StagingTable, projectID.String(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("summarize staging table: %w", err)
	}

	boundary := time.Unix(0, params.BoundaryUnixNano).UTC()
	windowStart := time.Unix(staging.MinTimeBucketUnixSec, 0).UTC()
	live, err := b.tableStats(ctx, attributeMetricsBackfillSpec.LiveTable, projectID.String(), &windowStart, &boundary)
	if err != nil {
		return nil, fmt.Errorf("summarize live table: %w", err)
	}

	return &ValidateAttributeMetricsBackfillResult{
		Staging: *staging,
		Live:    *live,
	}, nil
}

func (b *BackfillAttributeMetricsSummaries) tableStats(ctx context.Context, table string, projectID string, from *time.Time, to *time.Time) (*AttributeMetricsBackfillTableStats, error) {
	query := "SELECT count(), " +
		"toInt64(toUnixTimestamp(coalesce(min(time_bucket), toDateTime(0, 'UTC')))), " +
		"toInt64(toUnixTimestamp(coalesce(max(time_bucket), toDateTime(0, 'UTC')))), " +
		"sumIfMerge(total_cost), " +
		"sumIfMerge(total_input_tokens), " +
		"sumIfMerge(total_output_tokens), " +
		"sumIfMerge(total_tokens), " +
		"sumIfMerge(cache_read_input_tokens), " +
		"sumIfMerge(cache_creation_input_tokens), " +
		"countIfMerge(total_tool_calls), " +
		"uniqExactIfMerge(unique_tool_calls), " +
		"uniqExactIfMerge(total_chats) " +
		"FROM " + table + " WHERE gram_project_id = ?"
	args := []any{projectID}
	if from != nil {
		query += " AND time_bucket >= ?"
		args = append(args, *from)
	}
	if to != nil {
		query += " AND time_bucket < ?"
		args = append(args, *to)
	}

	var stats AttributeMetricsBackfillTableStats
	if err := b.conn.QueryRow(ctx, query, args...).Scan(
		&stats.RowCount,
		&stats.MinTimeBucketUnixSec,
		&stats.MaxTimeBucketUnixSec,
		&stats.TotalCost,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalTokens,
		&stats.CacheReadInputTokens,
		&stats.CacheCreationInputTokens,
		&stats.TotalToolCalls,
		&stats.UniqueToolCalls,
		&stats.TotalChats,
	); err != nil {
		return nil, fmt.Errorf("query %s stats: %w", table, err)
	}
	return &stats, nil
}

type ArchiveAttributeMetricsBackfillParams struct {
	ProjectID        string `json:"project_id"`
	BoundaryUnixNano int64  `json:"boundary_unix_nano"`
	// BackfillRunID scopes the archived rows to this workflow run so repeated
	// backfills of the same tenant stay distinguishable for a restore.
	BackfillRunID string `json:"backfill_run_id"`
}

type ArchiveAttributeMetricsBackfillResult struct {
	ArchivedRowCount uint64 `json:"archived_row_count"`
	// DeleteWindowStartUnixSec is min(time_bucket) of the staged rebuild — the
	// commit deletes live rows in [this, boundary). Live buckets older than the
	// staged coverage are never touched (the raw-retention clamp: summaries
	// whose raw logs already expired cannot be rebuilt, so they are not
	// deleted).
	DeleteWindowStartUnixSec int64 `json:"delete_window_start_unix_sec"`
}

// Archive snapshots the tenant's live rows in the delete window into the
// archive table. It must fully succeed before Commit runs so the snapshot is
// taken from untouched live data.
func (b *BackfillAttributeMetricsSummaries) Archive(ctx context.Context, params ArchiveAttributeMetricsBackfillParams) (*ArchiveAttributeMetricsBackfillResult, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	stop := startActivityHeartbeat(ctx)
	defer stop()

	archive, err := b.driver.Archive(ctx, projectID.String(), time.Unix(0, params.BoundaryUnixNano).UTC(), params.BackfillRunID)
	if err != nil {
		return nil, fmt.Errorf("archive live rows: %w", err)
	}

	b.logger.InfoContext(ctx, "archived live attribute metrics rows",
		attr.SlogProjectID(projectID.String()),
	)

	return &ArchiveAttributeMetricsBackfillResult{
		ArchivedRowCount:         archive.ArchivedRowCount,
		DeleteWindowStartUnixSec: archive.DeleteWindowStart.Unix(),
	}, nil
}

type CommitAttributeMetricsBackfillParams struct {
	ProjectID        string `json:"project_id"`
	BoundaryUnixNano int64  `json:"boundary_unix_nano"`
}

type CommitAttributeMetricsBackfillResult struct {
	DeleteWindowStartUnixSec int64 `json:"delete_window_start_unix_sec"`
	// InsertedRowCount is the tenant's live row count inside the swapped window
	// right after the insert (pre-merge, so it roughly matches staging's count).
	InsertedRowCount uint64 `json:"inserted_row_count"`
}

// Commit swaps the staged rebuild into the live table: delete the tenant's
// live rows in [staging min bucket, boundary), then insert the staged rows.
func (b *BackfillAttributeMetricsSummaries) Commit(ctx context.Context, params CommitAttributeMetricsBackfillParams) (*CommitAttributeMetricsBackfillResult, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project ID: %w", err)
	}

	stop := startActivityHeartbeat(ctx)
	defer stop()

	commit, err := b.driver.Commit(ctx, projectID.String(), time.Unix(0, params.BoundaryUnixNano).UTC())
	if err != nil {
		return nil, fmt.Errorf("commit backfill: %w", err)
	}

	b.logger.InfoContext(ctx, "committed attribute metrics backfill",
		attr.SlogProjectID(projectID.String()),
	)

	return &CommitAttributeMetricsBackfillResult{
		DeleteWindowStartUnixSec: commit.DeleteWindowStart.Unix(),
		InsertedRowCount:         commit.InsertedRowCount,
	}, nil
}

type CleanupAttributeMetricsBackfillParams struct {
	ProjectID string `json:"project_id"`
}

// Cleanup clears the tenant's staged rows. Runs after a commit and after an
// operator abort; a failure here never invalidates a committed swap.
func (b *BackfillAttributeMetricsSummaries) Cleanup(ctx context.Context, params CleanupAttributeMetricsBackfillParams) error {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project ID: %w", err)
	}

	stop := startActivityHeartbeat(ctx)
	defer stop()

	if err := b.driver.Cleanup(ctx, projectID.String()); err != nil {
		return fmt.Errorf("clean up staging rows: %w", err)
	}
	return nil
}
