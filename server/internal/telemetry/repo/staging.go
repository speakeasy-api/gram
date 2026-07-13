package repo

// telemetry_logs_staging is the holding pen for Claude OTEL api_request rows
// whose inline MCP attribution was redacted to "custom". Rows wait here until
// the transcript-derived attribution for their request_id arrives via hooks
// (or a timeout passes), then the promotion worker rewrites the attribution
// inside the attributes JSON and inserts the row into telemetry_logs — the
// moment attribute_metrics_summaries_mv fires — and deletes it here. Rows
// keep their id across promotion, so telemetry_logs itself is the
// exactly-once ledger: a promotion retry that finds the id already in
// telemetry_logs skips the insert.

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
}

// StagedTelemetrySession identifies one session with rows waiting in staging.
type StagedTelemetrySession struct {
	ProjectID string `ch:"project_id"`
	SessionID string `ch:"session_id"`
}

// InsertTelemetryLogsStaging inserts telemetry log records into the staging
// table in a single synchronous statement.
func (q *Queries) InsertTelemetryLogsStaging(ctx context.Context, args []InsertTelemetryLogParams) error {
	return q.insertTelemetryLogsInto(ctx, "telemetry_logs_staging", args)
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

// ListStagedTelemetryLogs returns every staged row for one session, complete
// enough to be re-inserted into telemetry_logs.
func (q *Queries) ListStagedTelemetryLogs(ctx context.Context, projectID string, sessionID string) ([]StagedTelemetryLogRow, error) {
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
	).
		From("telemetry_logs_staging").
		Where(squirrel.Eq{"gram_project_id": projectID}).
		Where(squirrel.Eq{"chat_id": sessionID}).
		OrderBy("time_unix_nano")

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

// ListStagedTelemetrySessions returns the distinct (project, session) pairs
// with rows currently waiting in staging, for the sweep to re-trigger
// promotion.
func (q *Queries) ListStagedTelemetrySessions(ctx context.Context) ([]StagedTelemetrySession, error) {
	query := "SELECT DISTINCT toString(gram_project_id) AS project_id, chat_id AS session_id FROM telemetry_logs_staging WHERE chat_id != ''"

	rows, err := q.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying staged telemetry sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []StagedTelemetrySession
	for rows.Next() {
		var row StagedTelemetrySession
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning staged telemetry session row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating staged telemetry session rows: %w", err)
	}
	return result, nil
}

// ListExistingTelemetryLogIDs returns which of the given ids already exist in
// telemetry_logs — the promotion dedup guard. The time window bounds the scan
// to the batch's own range (promotion preserves time_unix_nano), keeping the
// primary index effective.
func (q *Queries) ListExistingTelemetryLogIDs(ctx context.Context, projectID string, ids []string, minTimeUnixNano int64, maxTimeUnixNano int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

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
