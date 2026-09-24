package chrepo

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ?
// placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// OTelLogRow is a single row destined for the otel_logs table. See
// internal/otel/handler_log_ch_writer.go for how it is populated from a
// normalized gram.otel.v1.LogRecord.
type OTelLogRow struct {
	// OrganizationID and ProjectID are the tenancy stamped by the ingest edge
	// from authenticated state, carried on the record's provenance.
	OrganizationID string `ch:"organization_id"`
	ProjectID      string `ch:"project_id"`

	// TimeUnixNano is the effective event time: the producer's
	// time_unix_nano, falling back to observed time (then write time) when
	// the producer sent 0. It is the table's sort and TTL key so it must
	// never be epoch zero.
	TimeUnixNano         int64 `ch:"time_unix_nano"`
	ObservedTimeUnixNano int64 `ch:"observed_time_unix_nano"`

	// Source is the canonicalized producer surface derived from resource
	// service.name (e.g. claude-code, litellm), or unknown when absent.
	Source string `ch:"source"`

	// TraceID and SpanID are hex-encoded, empty when the record carries no
	// span context.
	TraceID string `ch:"trace_id"`
	SpanID  string `ch:"span_id"`

	EventName      string `ch:"event_name"`
	SeverityText   string `ch:"severity_text"`
	SeverityNumber int32  `ch:"severity_number"`

	// Body is the log body: string bodies verbatim, structured bodies
	// JSON-encoded.
	Body string `ch:"body"`

	// LogAttributes, ResourceAttributes and ScopeAttributes are stringified
	// JSON objects bound into the table's JSON columns.
	LogAttributes string `ch:"log_attributes"`
	Flags         uint32 `ch:"flags"`

	ResourceAttributes string `ch:"resource_attributes"`
	ResourceSchemaURL  string `ch:"resource_schema_url"`
	ScopeName          string `ch:"scope_name"`
	ScopeVersion       string `ch:"scope_version"`
	ScopeAttributes    string `ch:"scope_attributes"`
}

// OTelTraceRow is a single row destined for the otel_traces table. See
// internal/otel/handler_span_ch_writer.go for how it is populated from a
// normalized gram.otel.v1.Span.
type OTelTraceRow struct {
	// OrganizationID and ProjectID are the tenancy stamped by the ingest edge
	// from authenticated state, carried on the span's provenance.
	OrganizationID string `ch:"organization_id"`
	ProjectID      string `ch:"project_id"`

	// TimeUnixNano is the span start time, validated non-zero at the ingest
	// edge. DurationNano is end minus start.
	TimeUnixNano int64 `ch:"time_unix_nano"`
	DurationNano int64 `ch:"duration_nano"`

	// Source is the canonicalized producer surface derived from resource
	// service.name (e.g. claude-code, litellm), or unknown when absent.
	Source string `ch:"source"`

	// TraceID, SpanID and ParentSpanID are hex-encoded. ParentSpanID is empty
	// for root spans.
	TraceID      string `ch:"trace_id"`
	SpanID       string `ch:"span_id"`
	ParentSpanID string `ch:"parent_span_id"`

	SpanName string `ch:"span_name"`

	// SpanKind is the lowercase OTLP kind: unspecified, internal, server,
	// client, producer or consumer.
	SpanKind string `ch:"span_kind"`

	// StatusCode is the lowercase OTLP status: unspecified, ok or error.
	// StatusMessage is set only when StatusCode is error.
	StatusCode    string `ch:"status_code"`
	StatusMessage string `ch:"status_message"`
	TraceState    string `ch:"trace_state"`

	// SpanAttributes, ResourceAttributes and ScopeAttributes are stringified
	// JSON objects bound into the table's JSON columns.
	SpanAttributes string `ch:"span_attributes"`

	ResourceAttributes string `ch:"resource_attributes"`
	ResourceSchemaURL  string `ch:"resource_schema_url"`
	ScopeName          string `ch:"scope_name"`
	ScopeVersion       string `ch:"scope_version"`
	ScopeAttributes    string `ch:"scope_attributes"`
}

// otelLogColumns is the otel_logs column list, in the exact order
// InsertOTelLogs binds values. The derived timestamp column is deliberately
// absent so ClickHouse computes it from time_unix_nano.
var otelLogColumns = []string{
	"organization_id",
	"project_id",
	"time_unix_nano",
	"observed_time_unix_nano",
	"source",
	"trace_id",
	"span_id",
	"event_name",
	"severity_text",
	"severity_number",
	"body",
	"log_attributes",
	"flags",
	"resource_attributes",
	"resource_schema_url",
	"scope_name",
	"scope_version",
	"scope_attributes",
}

// otelTraceColumns is the otel_traces column list, in the exact order
// InsertOTelTraces binds values. The derived timestamp column is deliberately
// absent so ClickHouse computes it from time_unix_nano.
var otelTraceColumns = []string{
	"organization_id",
	"project_id",
	"time_unix_nano",
	"duration_nano",
	"source",
	"trace_id",
	"span_id",
	"parent_span_id",
	"span_name",
	"span_kind",
	"status_code",
	"status_message",
	"trace_state",
	"span_attributes",
	"resource_attributes",
	"resource_schema_url",
	"scope_name",
	"scope_version",
	"scope_attributes",
}

// chWriterInsertContext configures the async insert used by both event feed
// writers: the server may batch concurrent inserts, but the call waits for the
// flush so the Pub/Sub batch handler only acks durably accepted rows. Failed
// inserts are returned to the handler, which nacks the batch for redelivery —
// the tables are plain MergeTree, so redelivered rows land as duplicates that
// readers must tolerate.
func chWriterInsertContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 1,
	}))
}

// InsertOTelLogs writes a batch of log rows to otel_logs.
func (q *Queries) InsertOTelLogs(ctx context.Context, rows []OTelLogRow) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("otel_logs").Columns(otelLogColumns...)
	for _, row := range rows {
		builder = builder.Values(
			row.OrganizationID,
			row.ProjectID,
			row.TimeUnixNano,
			row.ObservedTimeUnixNano,
			row.Source,
			row.TraceID,
			row.SpanID,
			row.EventName,
			row.SeverityText,
			row.SeverityNumber,
			row.Body,
			row.LogAttributes,
			row.Flags,
			row.ResourceAttributes,
			row.ResourceSchemaURL,
			row.ScopeName,
			row.ScopeVersion,
			row.ScopeAttributes,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build otel_logs insert query: %w", err)
	}

	if err := q.conn.Exec(chWriterInsertContext(ctx), query, args...); err != nil {
		return fmt.Errorf("insert otel_logs: %w", err)
	}

	return nil
}

// InsertOTelTraces writes a batch of span rows to otel_traces.
func (q *Queries) InsertOTelTraces(ctx context.Context, rows []OTelTraceRow) error {
	if len(rows) == 0 {
		return nil
	}

	builder := sq.Insert("otel_traces").Columns(otelTraceColumns...)
	for _, row := range rows {
		builder = builder.Values(
			row.OrganizationID,
			row.ProjectID,
			row.TimeUnixNano,
			row.DurationNano,
			row.Source,
			row.TraceID,
			row.SpanID,
			row.ParentSpanID,
			row.SpanName,
			row.SpanKind,
			row.StatusCode,
			row.StatusMessage,
			row.TraceState,
			row.SpanAttributes,
			row.ResourceAttributes,
			row.ResourceSchemaURL,
			row.ScopeName,
			row.ScopeVersion,
			row.ScopeAttributes,
		)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("build otel_traces insert query: %w", err)
	}

	if err := q.conn.Exec(chWriterInsertContext(ctx), query, args...); err != nil {
		return fmt.Errorf("insert otel_traces: %w", err)
	}

	return nil
}
