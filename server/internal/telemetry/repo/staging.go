package repo

// telemetry_logs_staging is the holding pen for Claude OTEL api_request rows
// whose inline MCP attribution was redacted to "custom". Rows wait here until
// the transcript-derived attribution for their request_id arrives via hooks
// (or a timeout passes), then the promotion worker rewrites the attribution
// inside the attributes JSON and inserts the row into telemetry_logs — the
// moment attribute_metrics_summaries_mv fires — and deletes it here.
//
// Duplicate protection is layered, because a duplicate insert would re-fire
// the downstream MVs and double-count usage:
//
//  1. Promotion passes are serialized per project by the Temporal workflow ID.
//  2. Rows keep their id across promotion, and a pass skips ids that already
//     landed in telemetry_logs (a sequentially-consistent existence check, so
//     a retry after a crash between insert and delete does not re-insert).
//  3. Before inserting, a pass claims each row's promotion in Redis (SET NX,
//     see PromoteStagedTelemetry), so a race the existence check cannot see —
//     a timed-out attempt whose insert lands after the retry's check — cannot
//     double-insert: the retry loses the claim and defers the row. (Inserts
//     still carry a per-row insert_deduplication_token as a no-cost backstop
//     for any replicated/Cloud engine; it is inert on the non-replicated
//     MergeTree deployment, which is why the claim exists.)

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// StagedTelemetryLogRow is a full telemetry_logs_staging row, shaped so it can
// be re-inserted into telemetry_logs via InsertTelemetryLogParams. Attributes
// and ResourceAttributes are serialized with toJSONString so the promotion
// worker can patch them in Go and re-insert.
type StagedTelemetryLogRow struct {
	ID                   string  `ch:"id"`
	TimeUnixNano         int64   `ch:"time_unix_nano"`
	ObservedTimeUnixNano int64   `ch:"observed_time_unix_nano"`
	SeverityText         *string `ch:"severity_text"`
	Body                 string  `ch:"body"`
	TraceID              *string `ch:"trace_id"`
	SpanID               *string `ch:"span_id"`
	Attributes           string  `ch:"attributes"`
	ResourceAttributes   string  `ch:"resource_attributes"`
	GramProjectID        string  `ch:"gram_project_id"`
	GramDeploymentID     *string `ch:"gram_deployment_id"`
	GramFunctionID       *string `ch:"gram_function_id"`
	GramURN              string  `ch:"gram_urn"`
	ServiceName          string  `ch:"service_name"`
	ServiceVersion       *string `ch:"service_version"`
	GramChatID           *string `ch:"gram_chat_id"`
	RequestID            string  `ch:"request_id"`
	// OrgID scopes the attribution tuple lookup. The tuple's Redis key is
	// org-scoped (see telemetry.MCPAttributionTupleKey): the hooks key that
	// writes the tuple and the OTEL exporter key that staged this row can
	// resolve different projects, but always agree on the org.
	OrgID string `ch:"org_id"`
}

// stagedTelemetryRowsLimit bounds one promotion pass's batch so a large
// backlog cannot outlast the activity timeout. Oldest rows come first, and
// the promotion workflow drains page after page within one run (see
// promoteStagedTelemetryMaxPasses in the background package), so a backlog
// deeper than one page is scanned long before tuples expire rather than
// waiting one page per sweep tick.
const stagedTelemetryRowsLimit = 1000

// InsertTelemetryLogsStaging inserts telemetry log records into the staging
// table in a single synchronous statement.
func (q *Queries) InsertTelemetryLogsStaging(ctx context.Context, args []InsertTelemetryLogParams) error {
	return q.insertTelemetryLogsInto(ctx, "telemetry_logs_staging", args)
}

// InsertPromotedTelemetryLogs moves staged rows into telemetry_logs, one
// insert per row, each carrying a deterministic insert_deduplication_token
// derived from the row id. The token is a no-cost backstop on a replicated/
// Cloud engine; it is inert on the non-replicated MergeTree deployment, where
// the promotion path instead relies on a per-row Redis SET NX claim (see
// PromoteStagedTelemetry) to prevent a double insert. Promotion batches are
// tiny (a session's redacted rows), so per-row inserts are fine.
func (q *Queries) InsertPromotedTelemetryLogs(ctx context.Context, args []InsertTelemetryLogParams) error {
	for _, arg := range args {
		tokenCtx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
			"insert_deduplication_token": "promote:" + arg.ID,
		}))
		if err := q.insertTelemetryLogsInto(tokenCtx, "telemetry_logs", []InsertTelemetryLogParams{arg}); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queries) insertTelemetryLogsInto(ctx context.Context, table string, args []InsertTelemetryLogParams) error {
	if len(args) == 0 {
		return nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithAsync(false))

	builder := sq.Insert(table).
		Columns(
			"id",
			"time_unix_nano",
			"observed_time_unix_nano",
			"severity_text",
			"body",
			"trace_id",
			"span_id",
			"attributes",
			"resource_attributes",
			"gram_project_id",
			"gram_deployment_id",
			"gram_function_id",
			"gram_urn",
			"service_name",
			"service_version",
			"gram_chat_id",
		)

	for _, arg := range args {
		builder = builder.Values(
			arg.ID,
			arg.TimeUnixNano,
			arg.ObservedTimeUnixNano,
			arg.SeverityText,
			arg.Body,
			arg.TraceID,
			arg.SpanID,
			arg.Attributes,
			arg.ResourceAttributes,
			arg.GramProjectID,
			arg.GramDeploymentID,
			arg.GramFunctionID,
			arg.GramURN,
			arg.ServiceName,
			arg.ServiceVersion,
			arg.GramChatID,
		)
	}

	query, queryArgs, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building %s insert query: %w", table, err)
	}

	if err := q.conn.Exec(ctx, query, queryArgs...); err != nil {
		return fmt.Errorf("inserting into %s: %w", table, err)
	}

	return nil
}

// ListStagedTelemetryLogs returns staged rows for one project, oldest first
// and capped at stagedTelemetryRowsLimit, complete enough to be re-inserted
// into telemetry_logs.
func (q *Queries) ListStagedTelemetryLogs(ctx context.Context, projectID string) ([]StagedTelemetryLogRow, error) {
	sb := sq.Select(
		"toString(id) AS id",
		"time_unix_nano",
		"observed_time_unix_nano",
		"severity_text",
		"body",
		"toString(trace_id) AS trace_id",
		"toString(span_id) AS span_id",
		"toJSONString(attributes) AS attributes",
		"toJSONString(resource_attributes) AS resource_attributes",
		"toString(gram_project_id) AS gram_project_id",
		"toString(gram_deployment_id) AS gram_deployment_id",
		"toString(gram_function_id) AS gram_function_id",
		"gram_urn",
		"service_name",
		"service_version",
		"gram_chat_id",
		"request_id",
		"org_id",
	).
		From("telemetry_logs_staging").
		Where(squirrel.Eq{"gram_project_id": projectID}).
		OrderBy("observed_time_unix_nano").
		Limit(stagedTelemetryRowsLimit)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building staged telemetry logs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying staged telemetry logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []StagedTelemetryLogRow
	for rows.Next() {
		var row StagedTelemetryLogRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning staged telemetry log row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating staged telemetry log rows: %w", err)
	}
	return result, nil
}

// stagedTelemetryProjectsLimit bounds one dispatch pass's fan-out so a large
// backlog cannot outlast the dispatch workflow's run timeout. Promotion
// drains staging, so projects beyond the cap surface in the next pass
// (every 2 minutes) rather than starving.
const stagedTelemetryProjectsLimit = 1000

// ListStagedTelemetryProjects returns the distinct project ids with rows
// currently waiting in staging, for the scheduled dispatcher to fan out
// per-project promotion. Projects with the oldest staged rows come first, so
// timeout enforcement is not delayed by newer arrivals when the limit kicks
// in.
func (q *Queries) ListStagedTelemetryProjects(ctx context.Context) ([]string, error) {
	query := fmt.Sprintf(
		"SELECT toString(gram_project_id) AS project_id FROM telemetry_logs_staging GROUP BY project_id ORDER BY min(observed_time_unix_nano) LIMIT %d",
		stagedTelemetryProjectsLimit,
	)

	rows, err := q.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying staged telemetry projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("scanning staged telemetry project id: %w", err)
		}
		result = append(result, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating staged telemetry project rows: %w", err)
	}
	return result, nil
}

// ListExistingTelemetryLogIDs returns which of the given ids already exist in
// telemetry_logs — the promotion dedup guard. The time window bounds the scan
// to the batch's own range (promotion preserves time_unix_nano), keeping the
// primary index effective. The read demands sequential consistency so that on
// replicated setups a retry cannot miss an insert acknowledged by another
// replica moments earlier (harmless no-op on a single plain-MergeTree node).
func (q *Queries) ListExistingTelemetryLogIDs(ctx context.Context, projectID string, ids []string, minTimeUnixNano int64, maxTimeUnixNano int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"select_sequential_consistency": 1,
	}))

	sb := sq.Select("toString(id) AS id").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectID}).
		Where("time_unix_nano >= ?", minTimeUnixNano).
		Where("time_unix_nano <= ?", maxTimeUnixNano).
		Where(squirrel.Eq{"id": ids})

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building existing telemetry log ids query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying existing telemetry log ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning existing telemetry log id: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating existing telemetry log ids: %w", err)
	}
	return result, nil
}

// DeleteStagedTelemetryLogs removes promoted rows from staging (lightweight
// delete). Safe to retry: deleting an already-deleted id is a no-op, and a
// crash before this delete only leaves rows the next promotion pass skips via
// the dedup guard.
func (q *Queries) DeleteStagedTelemetryLogs(ctx context.Context, projectID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	sb := sq.Delete("telemetry_logs_staging").
		Where(squirrel.Eq{"gram_project_id": projectID}).
		Where(squirrel.Eq{"id": ids})

	query, args, err := sb.ToSql()
	if err != nil {
		return fmt.Errorf("building staged telemetry logs delete query: %w", err)
	}

	if err := q.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("deleting staged telemetry logs: %w", err)
	}
	return nil
}
